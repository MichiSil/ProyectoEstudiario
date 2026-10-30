package search

import (
	"context"
	"errors"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Loader trae de MongoDB los resúmenes de una página de resultados.
type Loader interface {
	GetMany(ctx context.Context, ids []string) ([]model.Summary, error)
}

// SolrSearch implementa service.SummarySearch: Solr decide qué resúmenes
// aparecen y en qué orden, y los datos se leen de MongoDB. Así la respuesta
// tiene la forma completa del contrato (autor, archivo, calificación) y
// siempre el dato actual de la fuente de verdad, sin guardar datos del
// autor en el índice (ADR-006).
type SolrSearch struct {
	Solr   *Solr
	Loader Loader
}

// Search busca entre los aprobados (RN-24). Si Solr no responde devuelve
// model.ErrSearchUnavailable (503 SEARCH_UNAVAILABLE): el detalle y la
// descarga siguen funcionando porque no pasan por Solr (ADR-010, ADR-014).
func (s *SolrSearch) Search(ctx context.Context, q model.SearchQuery) (model.Page, error) {
	page := model.Page{Page: q.Page, Size: q.Size, Items: []model.Summary{}}
	hits, total, err := s.Solr.Select(ctx, Query(q))
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return page, errors.Join(model.ErrSearchUnavailable, err)
		}
		return page, err
	}
	page.Total = total
	if len(hits) == 0 {
		return page, nil
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	docs, err := s.Loader.GetMany(ctx, ids)
	if err != nil {
		return page, err
	}
	byID := make(map[string]model.Summary, len(docs))
	for _, d := range docs {
		byID[d.ID] = d
	}
	// Se respeta el orden de Solr. Si el índice todavía tiene algo que en
	// MongoDB ya no está aprobado (por unos segundos, hasta la próxima
	// sincronización), no se muestra (RN-24).
	for _, id := range ids {
		if d, ok := byID[id]; ok && d.Status == model.StatusApproved {
			page.Items = append(page.Items, d)
		}
	}
	return page, nil
}
