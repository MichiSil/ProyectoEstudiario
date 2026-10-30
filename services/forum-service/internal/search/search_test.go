package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

func TestQueryTraduceFiltrosYOrden(t *testing.T) {
	p := Query(model.SearchQuery{
		Text: "integrales", Category: "analisis ii", Tags: []string{"parcial", "dobles"},
		MinRating: 3.5, Sort: model.SortRating, Page: 3, Size: 20,
	})
	if p.Get("q") != "integrales" || p.Get("defType") != "edismax" {
		t.Errorf("texto: q=%q defType=%q", p.Get("q"), p.Get("defType"))
	}
	wantFq := []string{"{!term f=categoryKey}analisis ii", "{!term f=tagKeys}parcial", "{!term f=tagKeys}dobles", "ratingAvg:[3.5 TO *]"}
	if !slices.Equal(p["fq"], wantFq) {
		t.Errorf("fq = %q, se esperaba %q", p["fq"], wantFq)
	}
	if p.Get("sort") != "ratingAvg desc, ratingCount desc, publishedAt desc, id asc" {
		t.Errorf("sort = %q", p.Get("sort"))
	}
	if p.Get("start") != "40" || p.Get("rows") != "20" {
		t.Errorf("página 3 de 20: start=%s rows=%s", p.Get("start"), p.Get("rows"))
	}
}

func TestQuerySinTextoOrdenaPorFechaAunquePidaRelevancia(t *testing.T) {
	p := Query(model.SearchQuery{Sort: model.SortRelevance, Page: 1, Size: 10})
	if p.Has("q") || p.Get("q.alt") != "*:*" {
		t.Errorf("sin texto tiene que traer todo: q=%q q.alt=%q", p.Get("q"), p.Get("q.alt"))
	}
	if p.Get("sort") != "publishedAt desc, id asc" {
		t.Errorf("sort = %q", p.Get("sort"))
	}
	if p := Query(model.SearchQuery{Text: "x", Sort: model.SortRelevance, Page: 1, Size: 10}); p.Get("sort") != "score desc, publishedAt desc, id asc" {
		t.Errorf("relevancia con texto: sort = %q", p.Get("sort"))
	}
}

func TestQueryNoInterpretaElFiltroComoSintaxisDeSolr(t *testing.T) {
	p := Query(model.SearchQuery{Category: `x" OR *:*`, Page: 1, Size: 10})
	if p.Get("fq") != `{!term f=categoryKey}x" OR *:*` {
		t.Errorf("fq = %q", p.Get("fq"))
	}
}

type fakeLoader map[string]model.Summary

func (f fakeLoader) GetMany(_ context.Context, ids []string) ([]model.Summary, error) {
	var out []model.Summary
	for _, id := range ids {
		if s, ok := f[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func TestSolrSearchRespetaElOrdenYOcultaLoQueYaNoEstaAprobado(t *testing.T) {
	solr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":{"numFound":4,"docs":[{"id":"b"},{"id":"a"},{"id":"c"},{"id":"d"}]}}`))
	}))
	defer solr.Close()
	s := &SolrSearch{Solr: NewSolr(solr.URL), Loader: fakeLoader{
		"a": {ID: "a", Status: model.StatusApproved},
		"b": {ID: "b", Status: model.StatusApproved},
		"c": {ID: "c", Status: model.StatusDeleted}, // eliminado hace un instante
		// d ya no existe
	}}
	page, err := s.Search(context.Background(), model.SearchQuery{Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range page.Items {
		ids = append(ids, it.ID)
	}
	if !slices.Equal(ids, []string{"b", "a"}) || page.Total != 4 {
		t.Errorf("ids = %v total = %d", ids, page.Total)
	}
}

func TestSolrCaidoEsBusquedaNoDisponible(t *testing.T) {
	solr := httptest.NewServer(http.NotFoundHandler())
	url := solr.URL
	solr.Close() // nadie escucha en esa dirección
	s := &SolrSearch{Solr: NewSolr(url), Loader: fakeLoader{}}
	if _, err := s.Search(context.Background(), model.SearchQuery{Page: 1, Size: 10}); !errors.Is(err, model.ErrSearchUnavailable) {
		t.Errorf("err = %v, se esperaba ErrSearchUnavailable", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer broken.Close()
	s.Solr = NewSolr(broken.URL)
	if _, err := s.Search(context.Background(), model.SearchQuery{Page: 1, Size: 10}); !errors.Is(err, model.ErrSearchUnavailable) {
		t.Errorf("Solr con 503: err = %v", err)
	}
}
