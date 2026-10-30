package repository_test

// Tests de integración contra un MongoDB real (requisito 2.11 del enunciado).
// Se ejecutan sólo si está definida FORUM_TEST_MONGO_URI:
//
//	docker compose up -d mongo mongo-init
//	FORUM_TEST_MONGO_URI='mongodb://forum_svc:forum-local@localhost:27017/forum?authSource=forum' go test ./internal/repository/

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/repository"
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

func event(t string) model.OutboxEvent {
	return model.OutboxEvent{EventID: newID(), EventType: t, EventVersion: 1, OccurredAt: time.Now().UTC(), Producer: "forum-service", Data: map[string]any{"x": 1}}
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
	if len(got.Outbox) != 1 || got.Outbox[0].PublishedAt != nil {
		t.Errorf("outbox = %+v, se esperaba un único evento sin publicar", got.Outbox)
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
