package service

import (
	"context"
	"errors"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// MySummaries devuelve las publicaciones del estudiante con su estado y, si
// fueron rechazadas, el motivo (RF-15).
func (s *Service) MySummaries(ctx context.Context, v Viewer, page, size int) (model.Page, error) {
	return s.Repo.ListByUser(ctx, v.UserID, page, size)
}

// Pending devuelve las publicaciones pendientes de la más vieja a la más nueva
// (RF-16). Sólo para administradores.
func (s *Service) Pending(ctx context.Context, v Viewer, page, size int) (model.Page, error) {
	if !v.Admin {
		return model.Page{}, model.ErrForbidden
	}
	return s.Repo.ListPending(ctx, page, size)
}

// SearchApproved busca entre los aprobados (RF-19, RF-20, RN-24). El orden por
// defecto es relevancia si hay texto y fecha si no.
func (s *Service) SearchApproved(ctx context.Context, q model.SearchQuery) (model.Page, error) {
	if q.Sort == "" {
		q.Sort = model.SortDate
		if q.Text != "" {
			q.Sort = model.SortRelevance
		}
	}
	page, err := s.Search.Search(ctx, q)
	if err != nil && !errors.Is(err, model.ErrSearchUnavailable) && errors.Is(err, model.ErrUnavailable) {
		return page, errors.Join(model.ErrSearchUnavailable, err)
	}
	return page, err
}

// Rate guarda la calificación de 1 a 5 de un estudiante (RF-22): sólo de
// publicaciones aprobadas (RN-28), nunca de las propias (RN-29), y si ya la
// había calificado reemplaza su puntaje (RN-30).
func (s *Service) Rate(ctx context.Context, v Viewer, id string, score int) (model.Rating, error) {
	if score < 1 || score > 5 {
		return model.Rating{}, &model.ValidationError{Fields: []model.FieldError{{Field: "score", Message: "tiene que ser un número entero de 1 a 5"}}}
	}
	doc, err := s.Get(ctx, v, id)
	if err != nil {
		return model.Rating{}, err
	}
	if doc.Status != model.StatusApproved {
		return model.Rating{}, model.ErrNotApproved
	}
	if doc.Author.UserID != "" && doc.Author.UserID == v.UserID {
		return model.Rating{}, model.ErrOwnSummary
	}
	return s.Repo.Rate(ctx, id, v.UserID, score, s.now())
}
