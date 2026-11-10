package search

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// DefaultSkipFor es cuánto se saltea Solr después de que falla un listado: en
// ese tiempo los listados van directo a MongoDB, sin esperar otra vez los 2 s
// del timeout de Solr. Pasado ese tiempo, el próximo listado vuelve a probar.
const DefaultSkipFor = 10 * time.Second

// listFallbacks cuenta los listados que salieron de MongoDB porque Solr no
// respondió (ADR-006, ADR-011).
var listFallbacks = promauto.NewCounter(prometheus.CounterOpts{
	Name: "forum_search_list_fallback_total",
	Help: "Listados de aprobados sin texto que se leyeron de MongoDB porque Solr no estaba disponible (ADR-006).",
})

// Searcher es una búsqueda de aprobados (la misma interfaz que service.SummarySearch).
type Searcher interface {
	Search(ctx context.Context, q model.SearchQuery) (model.Page, error)
}

// ListFallback hace que el listado de aprobados sin texto (la página principal
// del foro y GET /v1/documents sin q) no dependa de Solr: si Primary (Solr) no
// está disponible, lo lee de Fallback (MongoDB) con los mismos filtros
// (materia, etiquetas, calificación mínima), el mismo orden y la misma
// paginación. La búsqueda por texto no tiene alternativa: sigue respondiendo
// ErrSearchUnavailable (503 SEARCH_UNAVAILABLE).
type ListFallback struct {
	Primary  Searcher
	Fallback Searcher
	Logger   *slog.Logger
	// SkipFor es cuánto se saltea Solr para los listados después de una falla
	// (0: DefaultSkipFor).
	SkipFor time.Duration
	Now     func() time.Time

	skipUntil atomic.Int64 // UnixNano hasta el que los listados no prueban Solr
}

func (f *ListFallback) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *ListFallback) Search(ctx context.Context, q model.SearchQuery) (model.Page, error) {
	if q.Text != "" {
		return f.Primary.Search(ctx, q)
	}
	if f.now().UnixNano() < f.skipUntil.Load() {
		return f.fallback(ctx, q, nil)
	}
	page, err := f.Primary.Search(ctx, q)
	if err == nil || !errors.Is(err, model.ErrSearchUnavailable) {
		return page, err
	}
	f.skipUntil.Store(f.now().Add(f.skipFor()).UnixNano())
	return f.fallback(ctx, q, err)
}

// fallback lee el listado de MongoDB. solrErr es el error de Solr que lo
// provocó (nil si Solr se está salteando por una falla reciente).
func (f *ListFallback) fallback(ctx context.Context, q model.SearchQuery, solrErr error) (model.Page, error) {
	listFallbacks.Inc()
	if solrErr != nil {
		f.Logger.WarnContext(ctx, "Solr no disponible: el listado se lee de MongoDB",
			"error", solrErr.Error(), "skipForMs", f.skipFor().Milliseconds())
	}
	return f.Fallback.Search(ctx, q)
}

func (f *ListFallback) skipFor() time.Duration {
	if f.SkipFor > 0 {
		return f.SkipFor
	}
	return DefaultSkipFor
}
