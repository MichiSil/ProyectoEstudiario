package search

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// fakeSearcher cuenta las llamadas y responde err o una página con source.
type fakeSearcher struct {
	err   error
	calls int
	name  string
}

func (f *fakeSearcher) Search(_ context.Context, q model.SearchQuery) (model.Page, error) {
	f.calls++
	if f.err != nil {
		return model.Page{}, f.err
	}
	return model.Page{Page: q.Page, Size: q.Size, Total: 1, Items: []model.Summary{{ID: f.name}}}, nil
}

func solrDown() error {
	return errors.Join(model.ErrSearchUnavailable, ErrUnavailable)
}

func newFallback(primary, mongo *fakeSearcher, now *time.Time) *ListFallback {
	return &ListFallback{
		Primary: primary, Fallback: mongo, SkipFor: 10 * time.Second,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:    func() time.Time { return *now },
	}
}

func TestListadoSinTextoSaleDeMongoSiSolrNoEsta(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	solr, mongo := &fakeSearcher{err: solrDown(), name: "solr"}, &fakeSearcher{name: "mongo"}
	f := newFallback(solr, mongo, &now)
	before := testutil.ToFloat64(listFallbacks)
	q := model.SearchQuery{Category: "analisis ii", Sort: model.SortDate, Page: 2, Size: 20}

	page, err := f.Search(context.Background(), q)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "mongo" || page.Page != 2 || page.Size != 20 {
		t.Fatalf("listado con Solr caído = %+v, %v; se esperaba la página de MongoDB", page, err)
	}

	// En los 10 s siguientes no se vuelve a esperar a Solr.
	now = now.Add(5 * time.Second)
	if _, err := f.Search(context.Background(), q); err != nil || solr.calls != 1 || mongo.calls != 2 {
		t.Errorf("durante la espera: Solr %d llamadas, MongoDB %d, err %v; se esperaba no llamar a Solr", solr.calls, mongo.calls, err)
	}
	if got := testutil.ToFloat64(listFallbacks) - before; got != 2 {
		t.Errorf("forum_search_list_fallback_total = %v, se esperaban 2", got)
	}

	// Pasado ese tiempo vuelve a probar Solr y, si volvió, lo usa.
	now = now.Add(6 * time.Second)
	solr.err = nil
	if page, err := f.Search(context.Background(), q); err != nil || page.Items[0].ID != "solr" || solr.calls != 2 {
		t.Errorf("con Solr de vuelta = %+v, %v (Solr %d llamadas); se esperaba usar Solr", page, err, solr.calls)
	}
}

func TestBusquedaConTextoSigueDependiendoDeSolr(t *testing.T) {
	now := time.Now()
	solr, mongo := &fakeSearcher{err: solrDown()}, &fakeSearcher{}
	f := newFallback(solr, mongo, &now)
	_, err := f.Search(context.Background(), model.SearchQuery{Text: "integrales", Page: 1, Size: 20})
	if !errors.Is(err, model.ErrSearchUnavailable) || mongo.calls != 0 {
		t.Errorf("búsqueda con texto: err = %v, MongoDB %d llamadas; se esperaba 503 sin ir a MongoDB", err, mongo.calls)
	}
}

func TestOtroErrorDeSolrNoUsaMongo(t *testing.T) {
	now := time.Now()
	solr, mongo := &fakeSearcher{err: errors.New("respuesta inválida")}, &fakeSearcher{}
	f := newFallback(solr, mongo, &now)
	if _, err := f.Search(context.Background(), model.SearchQuery{Page: 1, Size: 20}); err == nil || mongo.calls != 0 {
		t.Errorf("err = %v, MongoDB %d llamadas; un error que no es de disponibilidad no se oculta", err, mongo.calls)
	}
}
