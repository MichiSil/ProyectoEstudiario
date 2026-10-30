package search

// Tests de integración contra un Solr real (requisito 2.11 del enunciado).
// Se ejecutan sólo si está definida FORUM_TEST_SOLR_URL, la URL de un core.
// El test vacía ese core, así que se corre contra un Solr descartable, no
// contra el del docker compose:
//
//	docker run -d --rm --name solr-test -p 18983:8983 solr:9.7.0 solr-precreate summaries_test
//	FORUM_TEST_SOLR_URL=http://localhost:18983/solr/summaries_test go test ./internal/search/
//	docker stop solr-test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

func newTestSolr(t *testing.T) *Solr {
	t.Helper()
	core := os.Getenv("FORUM_TEST_SOLR_URL")
	if core == "" {
		t.Skip("FORUM_TEST_SOLR_URL no definida: se omiten los tests de integración con Solr")
	}
	solr := NewSolr(core)
	// Dos veces: aplicar el esquema tiene que ser idempotente.
	for range 2 {
		if err := solr.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("esquema: %v", err)
		}
	}
	if err := solr.DeleteAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	return solr
}

// fakeSource es MongoDB en memoria para el indexador.
type fakeSource map[string]model.Summary

func (f fakeSource) ChangedSince(_ context.Context, since time.Time) ([]model.Summary, error) {
	var out []model.Summary
	for _, s := range f {
		if !s.UpdatedAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f fakeSource) EachApproved(_ context.Context, fn func(model.Summary) error) error {
	for _, s := range f {
		if s.Status == model.StatusApproved {
			if err := fn(s); err != nil {
				return err
			}
		}
	}
	return nil
}

func summary(id, title, category string, tags []string, status model.Status, avg float64, count int, published time.Time) model.Summary {
	keys := make([]string, len(tags))
	for i, t := range tags {
		keys[i] = model.NormalizeKey(t)
	}
	return model.Summary{
		ID: id, Title: title, Description: "Resumen de " + title, Category: category,
		CategoryKey: model.NormalizeKey(category), Tags: tags, TagKeys: keys,
		Source: model.SourceEstudiario, Status: status, Rating: model.Rating{Average: avg, Count: count},
		CreatedAt: published, UpdatedAt: published, PublishedAt: &published,
	}
}

// ids busca y espera hasta 10 s a que el resultado sea el esperado (commitWithin es 5 s).
func ids(t *testing.T, solr *Solr, q model.SearchQuery, want []string, ordered bool) {
	t.Helper()
	if q.Page == 0 {
		q.Page, q.Size = 1, 50
	}
	var got []string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		hits, _, err := solr.Select(context.Background(), Query(q))
		if err != nil {
			t.Fatal(err)
		}
		got = got[:0]
		for _, h := range hits {
			got = append(got, h.ID)
		}
		cmp := slices.Clone(got)
		w := slices.Clone(want)
		if !ordered {
			sort.Strings(cmp)
			sort.Strings(w)
		}
		if slices.Equal(cmp, w) {
			return
		}
	}
	t.Errorf("búsqueda %+v: ids = %v, se esperaba %v", q, got, want)
}

func TestIntegracionIndexadorYBusqueda(t *testing.T) {
	solr := newTestSolr(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	src := fakeSource{
		"am2":  summary("am2", "Integrales dobles y triples", "Análisis Matemático II", []string{"Parcial", "integrales"}, model.StatusApproved, 4.5, 40, base),
		"am2b": summary("am2b", "Series de potencias", "Análisis Matemático II", []string{"final"}, model.StatusApproved, 4.5, 2, base.Add(time.Minute)),
		"fis":  summary("fis", "Cinemática del punto", "Física I", []string{"parcial"}, model.StatusApproved, 3, 10, base.Add(2*time.Minute)),
		"pen":  summary("pen", "Integrales pendientes", "Análisis Matemático II", nil, model.StatusPending, 0, 0, base),
	}
	ix := &Indexer{Solr: solr, Source: src, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := ix.Sync(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// Sólo los aprobados (RN-24). Sin tildes y en singular también encuentra (análisis en español).
	ids(t, solr, model.SearchQuery{Text: "integral"}, []string{"am2"}, true)
	ids(t, solr, model.SearchQuery{Text: "cinematica"}, []string{"fis"}, true)
	ids(t, solr, model.SearchQuery{Text: "analisis"}, []string{"am2", "am2b"}, false)
	// Filtro por categoría y etiqueta, sin importar mayúsculas ni tildes.
	ids(t, solr, model.SearchQuery{Category: model.NormalizeKey("analisis matematico ii")}, []string{"am2b", "am2"}, true)
	ids(t, solr, model.SearchQuery{Tags: []string{"parcial"}}, []string{"fis", "am2"}, true)
	// CA-21: materia + orden por calificación, a igual promedio gana el que tiene más votos.
	ids(t, solr, model.SearchQuery{Category: model.NormalizeKey("Análisis Matemático II"), Sort: model.SortRating}, []string{"am2", "am2b"}, true)
	ids(t, solr, model.SearchQuery{MinRating: 4}, []string{"am2", "am2b"}, false)
	// Paginación.
	ids(t, solr, model.SearchQuery{Sort: model.SortDate, Page: 2, Size: 2}, []string{"am2"}, true)

	// Se elimina uno y se aprueba el pendiente: la próxima sincronización lo refleja (RN-26).
	later := time.Now().UTC()
	fis := src["fis"]
	fis.Status, fis.UpdatedAt = model.StatusDeleted, later
	src["fis"] = fis
	pen := src["pen"]
	pen.Status, pen.UpdatedAt, pen.PublishedAt = model.StatusApproved, later, &later
	src["pen"] = pen
	if err := ix.Sync(ctx, later.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	ids(t, solr, model.SearchQuery{Text: "integrales"}, []string{"am2", "pen"}, false)
	ids(t, solr, model.SearchQuery{Text: "cinematica"}, []string{}, true)

	// La reconciliación corrige lo que se escapó: una calificación nueva, un
	// documento que falta y uno que sobra.
	am2b := src["am2b"]
	am2b.Rating = model.Rating{Average: 5, Count: 3}
	src["am2b"] = am2b
	if err := solr.Delete(ctx, []string{"pen"}); err != nil {
		t.Fatal(err)
	}
	if err := solr.Upsert(ctx, []Doc{ToDoc(summary("viejo", "Borrado hace rato", "Química", nil, model.StatusApproved, 0, 0, base))}); err != nil {
		t.Fatal(err)
	}
	ids(t, solr, model.SearchQuery{Text: "borrado"}, []string{"viejo"}, true)
	if err := ix.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	ids(t, solr, model.SearchQuery{Sort: model.SortRating, Category: model.NormalizeKey("Análisis Matemático II")}, []string{"am2b", "am2", "pen"}, true)
	ids(t, solr, model.SearchQuery{Text: "borrado"}, []string{}, true)

	// Reconstrucción completa desde cero.
	n, err := ix.Rebuild(ctx)
	if err != nil || n != 3 {
		t.Fatalf("rebuild: n=%d err=%v", n, err)
	}
	ids(t, solr, model.SearchQuery{}, []string{"am2", "am2b", "pen"}, false)
}
