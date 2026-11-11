package repository_test

// Tests de integración contra un MongoDB real (requisito 2.11 del enunciado).
// Se ejecutan sólo si está definida FORUM_TEST_MONGO_URI:
//
// Los puertos de MongoDB no se publican fuera de Docker: se corren en un
// contenedor conectado a la red de docker compose (ver el README):
//
//	docker run --rm --network estudiario_default -v "$PWD/services/forum-service:/src" -w /src \
//	  -e FORUM_TEST_MONGO_URI='mongodb://forum_svc:forum-local@mongo:27017/forum?authSource=forum' \
//	  golang:1.26-alpine go test ./internal/repository/

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/repository"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/search"
)

func newDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("FORUM_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("FORUM_TEST_MONGO_URI no definida: se omiten los tests de integración con MongoDB")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	db := client.Database("forum")
	if err := repository.New(db).EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("índices: %v", err)
	}
	// Los datos de prueba llevan la marca testAuthor: se borran al terminar,
	// para no dejar pendientes de prueba en la base de desarrollo.
	t.Cleanup(func() {
		ctx := context.Background()
		cur, err := db.Collection("summaries").Find(ctx, bson.D{{Key: "author.name", Value: testAuthor}})
		if err == nil {
			var docs []model.Summary
			_ = cur.All(ctx, &docs)
			for _, d := range docs {
				_, _ = db.Collection("ratings").DeleteMany(ctx, bson.D{{Key: "summaryId", Value: d.ID}})
			}
		}
		_, _ = db.Collection("summaries").DeleteMany(ctx, bson.D{{Key: "author.name", Value: testAuthor}})
		_, _ = db.Collection("idempotency").DeleteMany(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$regex", Value: "^test-integracion:"}}}})
	})
	return db
}

// testAuthor marca los datos que crean estos tests.
const testAuthor = "test-integracion"

func newID() string { return strings.ToLower(rand.Text()) }

// newSummary arma un resumen pendiente con una categoría única por test, para
// no depender de los datos de otros tests.
func newSummary(category string) model.Summary {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return model.Summary{
		ID: newID(), Title: "Integrales dobles", Description: "Teoría y ejercicios resueltos",
		Category: category, CategoryKey: model.NormalizeKey(category),
		Tags: []string{"Parcial"}, TagKeys: []string{"parcial"},
		Author: model.Author{UserID: "autor-" + newID(), Name: testAuthor}, Source: model.SourceEstudiario,
		Status: model.StatusPending, File: model.FileRef{Key: "k", ContentType: "application/pdf", SizeBytes: 10},
		CreatedAt: now, UpdatedAt: now,
	}
}

// event arma un evento de prueba ya marcado como publicado: si el sistema está
// levantado mientras corren estos tests, el relay de forum-service no lo manda
// a RabbitMQ (sus datos son falsos y los consumidores lo mandarían a la DLQ).
func event(t string) model.OutboxEvent {
	now := time.Now().UTC()
	return model.OutboxEvent{EventID: newID(), EventType: t, EventVersion: 1, OccurredAt: now, Producer: "forum-service",
		Data: map[string]any{"x": 1}, PublishedAt: &now}
}

func TestDosModeracionesSimultaneasSoloGanaUna(t *testing.T) {
	db := newDB(t)
	repo, ctx := repository.New(db), context.Background()
	s := newSummary("rn23-" + newID())
	if err := repo.Create(ctx, s); err != nil {
		t.Fatal(err)
	}

	// 10 administradores aprueban o rechazan a la vez (RN-23, CA-18).
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mod := model.Moderation{To: model.StatusApproved, By: "admin", At: time.Now().UTC(), Event: event(model.EventApproved)}
			if i%2 == 1 {
				mod = model.Moderation{To: model.StatusRejected, Reason: "no", By: "admin", At: time.Now().UTC(), Event: event(model.EventRejected)}
			}
			errs[i] = repo.Moderate(ctx, s.ID, mod)
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, model.ErrAlreadyModerated):
			t.Fatalf("error inesperado: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d moderaciones tuvieron efecto, se esperaba exactamente 1", ok)
	}
	got, err := repo.Get(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Outbox) != 1 {
		t.Errorf("outbox = %+v, se esperaba un único evento", got.Outbox)
	}
	if (got.Status == model.StatusApproved) != (got.Outbox[0].EventType == model.EventApproved) {
		t.Errorf("el estado %q no coincide con el evento %q", got.Status, got.Outbox[0].EventType)
	}
}

func TestModerarAlgoEliminadoOInexistenteEsNotFound(t *testing.T) {
	db := newDB(t)
	repo, ctx := repository.New(db), context.Background()
	s := newSummary("del-" + newID())
	_ = repo.Create(ctx, s)
	if err := repo.Delete(ctx, s.ID, "admin", time.Now().UTC(), event(model.EventDeleted)); err != nil {
		t.Fatal(err)
	}
	mod := model.Moderation{To: model.StatusApproved, By: "admin", At: time.Now().UTC(), Event: event(model.EventApproved)}
	if err := repo.Moderate(ctx, s.ID, mod); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("moderar un eliminado: err = %v, se esperaba ErrNotFound", err)
	}
	if err := repo.Delete(ctx, s.ID, "admin", time.Now().UTC(), event(model.EventDeleted)); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("eliminar dos veces: err = %v, se esperaba ErrNotFound", err)
	}
	if err := repo.Moderate(ctx, "no-existe", mod); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("moderar inexistente: err = %v, se esperaba ErrNotFound", err)
	}
}

func TestCalificarReemplazaElPuntajeAnterior(t *testing.T) {
	db := newDB(t)
	repo, ctx := repository.New(db), context.Background()
	s := newSummary("rating-" + newID())
	_ = repo.Create(ctx, s)

	_, _ = repo.Rate(ctx, s.ID, "ana", 2, time.Now())
	_, _ = repo.Rate(ctx, s.ID, "beto", 4, time.Now())
	r, err := repo.Rate(ctx, s.ID, "ana", 5, time.Now()) // CA-22: reemplaza el 2
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 2 || r.Average != 4.5 {
		t.Fatalf("rating = %+v, se esperaba promedio 4.5 con 2 calificaciones (RN-30, RN-31)", r)
	}
	got, _ := repo.Get(ctx, s.ID)
	if got.Rating != r {
		t.Errorf("el resumen guardó %+v, se esperaba %+v", got.Rating, r)
	}
}

func TestBusquedaSoloAprobadosConFiltrosYOrden(t *testing.T) {
	db := newDB(t)
	repo, search, ctx := repository.New(db), repository.NewSearch(db), context.Background()
	category := "Análisis " + newID()

	mk := func(title string, approve bool, ratings ...int) model.Summary {
		s := newSummary(category)
		s.Title = title
		_ = repo.Create(ctx, s)
		if approve {
			_ = repo.Moderate(ctx, s.ID, model.Moderation{To: model.StatusApproved, By: "a", At: time.Now().UTC(), Event: event(model.EventApproved)})
		}
		for i, sc := range ratings {
			_, _ = repo.Rate(ctx, s.ID, "u"+string(rune('a'+i)), sc, time.Now())
		}
		return s
	}
	bajo := mk("Guía de series", true, 2)
	alto := mk("Guía de integrales", true, 5, 4)
	mk("Pendiente de integrales", false, 5)

	q := model.SearchQuery{Category: model.NormalizeKey(category), Sort: model.SortRating, Page: 1, Size: 10}
	page, err := search.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].ID != alto.ID || page.Items[1].ID != bajo.ID {
		t.Fatalf("resultado = %d items (total %d), se esperaban los 2 aprobados del mejor al peor calificado (CA-21)", len(page.Items), page.Total)
	}

	q.MinRating = 4
	if page, _ = search.Search(ctx, q); page.Total != 1 || page.Items[0].ID != alto.ID {
		t.Errorf("con minRating 4: total = %d, se esperaba sólo el de 4.5", page.Total)
	}

	q = model.SearchQuery{Text: "integrales", Category: model.NormalizeKey(category), Sort: model.SortRelevance, Page: 1, Size: 10}
	if page, _ = search.Search(ctx, q); page.Total != 1 || page.Items[0].ID != alto.ID {
		t.Errorf("texto 'integrales': total = %d, se esperaba sólo el aprobado que lo menciona", page.Total)
	}
}

func TestIdempotenciaReservaUnaSolaVez(t *testing.T) {
	db := newDB(t)
	repo, ctx := repository.New(db), context.Background()
	rec := model.IdempotencyRecord{ID: "test-integracion:" + newID(), RequestHash: "h1", CreatedAt: time.Now().UTC()}

	if _, reserved, err := repo.ReserveIdempotency(ctx, rec); err != nil || !reserved {
		t.Fatalf("primera reserva: reserved = %v, err = %v", reserved, err)
	}
	existing, reserved, err := repo.ReserveIdempotency(ctx, rec)
	if err != nil || reserved || existing.RequestHash != "h1" || existing.Done {
		t.Fatalf("segunda reserva: reserved = %v, existing = %+v, err = %v", reserved, existing, err)
	}
	if err := repo.CompleteIdempotency(ctx, rec.ID, "doc-1"); err != nil {
		t.Fatal(err)
	}
	existing, _, _ = repo.ReserveIdempotency(ctx, rec)
	if !existing.Done || existing.DocumentID != "doc-1" {
		t.Errorf("después de completar: %+v", existing)
	}
}

// solrCaido es Solr sin responder, para probar el listado desde MongoDB.
type solrCaido struct{}

func (solrCaido) Search(context.Context, model.SearchQuery) (model.Page, error) {
	return model.Page{}, errors.Join(model.ErrSearchUnavailable, errors.New("solr no responde"))
}

// POSTMORTEM, acción 2: con Solr caído, el listado sin texto sale de MongoDB
// con los mismos filtros, orden y paginación (ADR-006).
func TestListadoDesdeMongoConSolrCaido(t *testing.T) {
	db := newDB(t)
	repo, ctx := repository.New(db), context.Background()
	list := &search.ListFallback{Primary: solrCaido{}, Fallback: repository.NewSearch(db), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	category := "Física " + newID()

	approve := func(title string, tags []string, score int, publishedAt time.Time) model.Summary {
		s := newSummary(category)
		s.Title, s.Tags, s.TagKeys = title, tags, tags
		if err := repo.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := repo.Moderate(ctx, s.ID, model.Moderation{To: model.StatusApproved, By: "a", At: publishedAt, Event: event(model.EventApproved)}); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Rate(ctx, s.ID, "u-"+newID(), score, time.Now()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	viejo := approve("Cinemática", []string{"parcial"}, 5, t0.Add(-2*time.Hour))
	medio := approve("Dinámica", []string{"final"}, 2, t0.Add(-time.Hour))
	nuevo := approve("Energía", []string{"parcial"}, 4, t0)
	pendiente := newSummary(category)
	_ = repo.Create(ctx, pendiente)

	ids := func(p model.Page) []string {
		out := []string{}
		for _, s := range p.Items {
			out = append(out, s.ID)
		}
		return out
	}
	key := model.NormalizeKey(category)
	for _, c := range []struct {
		name  string
		q     model.SearchQuery
		total int
		want  []string
	}{
		{"por fecha", model.SearchQuery{Category: key, Sort: model.SortDate, Page: 1, Size: 10}, 3, []string{nuevo.ID, medio.ID, viejo.ID}},
		{"por calificación", model.SearchQuery{Category: key, Sort: model.SortRating, Page: 1, Size: 10}, 3, []string{viejo.ID, nuevo.ID, medio.ID}},
		{"segunda página", model.SearchQuery{Category: key, Sort: model.SortDate, Page: 2, Size: 2}, 3, []string{viejo.ID}},
		{"etiqueta", model.SearchQuery{Category: key, Tags: []string{"parcial"}, Sort: model.SortDate, Page: 1, Size: 10}, 2, []string{nuevo.ID, viejo.ID}},
		{"calificación mínima", model.SearchQuery{Category: key, MinRating: 4, Sort: model.SortDate, Page: 1, Size: 10}, 2, []string{nuevo.ID, viejo.ID}},
	} {
		page, err := list.Search(ctx, c.q)
		if err != nil || page.Total != c.total || !slices.Equal(ids(page), c.want) {
			t.Errorf("%s: total %d, ids %v, err %v; se esperaban total %d e ids %v", c.name, page.Total, ids(page), err, c.total, c.want)
		}
	}
}

// Calificar cambia el updatedAt del resumen junto con el promedio: así el
// indexador lo pasa a Solr en el próximo ciclo (unos 5 s) y el filtro por
// calificación mínima lo ve enseguida, sin esperar la reconciliación.
func TestCalificarAvisaAlIndexador(t *testing.T) {
	db := newDB(t)
	repo, ctx := repository.New(db), context.Background()
	s := newSummary("rating-indice-" + newID())
	_ = repo.Create(ctx, s)
	_ = repo.Moderate(ctx, s.ID, model.Moderation{To: model.StatusApproved, By: "a", At: time.Now().UTC(), Event: event(model.EventApproved)})

	since := time.Now().UTC().Add(time.Second).Truncate(time.Millisecond)
	ratedAt := since.Add(time.Second)
	if _, err := repo.Rate(ctx, s.ID, "ana", 4, ratedAt); err != nil {
		t.Fatal(err)
	}
	changed, err := repo.ChangedSince(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range changed {
		if c.ID == s.ID {
			found = true
			if c.Rating.Average != 4 || !c.UpdatedAt.Equal(ratedAt) {
				t.Errorf("cambio = rating %+v, updatedAt %v; se esperaba 4 y %v", c.Rating, c.UpdatedAt, ratedAt)
			}
		}
	}
	if !found {
		t.Error("el resumen calificado no aparece en ChangedSince: el indexador no se entera hasta la reconciliación")
	}
}
