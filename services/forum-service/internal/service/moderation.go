package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/correlation"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// MaxReasonLen es el largo máximo del motivo de rechazo.
const MaxReasonLen = 1000

// Approve aprueba una publicación pendiente (RF-17, RN-21). Si ya no está
// pendiente (por ejemplo, otro administrador la moderó un instante antes)
// devuelve ErrAlreadyModerated (RN-23).
func (s *Service) Approve(ctx context.Context, v Viewer, id string) error {
	return s.moderate(ctx, v, id, model.StatusApproved, "")
}

// Reject rechaza una publicación pendiente con su motivo (RF-17, RN-22).
func (s *Service) Reject(ctx context.Context, v Viewer, id, reason string) error {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return &model.ValidationError{Fields: []model.FieldError{{Field: "reason", Message: "rechazar exige un motivo"}}}
	case utf8.RuneCountInString(reason) > MaxReasonLen:
		return &model.ValidationError{Fields: []model.FieldError{{Field: "reason", Message: "el motivo tiene como máximo 1000 caracteres"}}}
	}
	return s.moderate(ctx, v, id, model.StatusRejected, reason)
}

func (s *Service) moderate(ctx context.Context, v Viewer, id string, to model.Status, reason string) error {
	if !v.Admin {
		return model.ErrForbidden
	}
	// Los datos del evento salen del documento. Si entre esta lectura y la
	// actualización otro administrador lo moderó, la actualización condicional
	// no lo encuentra pendiente y no se guarda nada (RN-23).
	doc, err := s.Repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !doc.Visible() {
		return model.ErrNotFound
	}
	if doc.Status != model.StatusPending {
		return model.ErrAlreadyModerated
	}
	at := s.now()
	eventType := model.EventApproved
	if to == model.StatusRejected {
		eventType = model.EventRejected
	}
	return s.Repo.Moderate(ctx, id, model.Moderation{
		To: to, Reason: reason, By: v.UserID, At: at,
		Event: s.event(ctx, eventType, at, moderationData(doc, v.UserID, at, to, reason)),
	})
}

// Delete elimina una publicación en cualquier estado (RF-18). Deja de verse en
// el foro, en la búsqueda y en la capacidad externa (RN-24, RN-39).
func (s *Service) Delete(ctx context.Context, v Viewer, id string) error {
	if !v.Admin {
		return model.ErrForbidden
	}
	doc, err := s.Repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !doc.Visible() {
		return model.ErrNotFound
	}
	at := s.now()
	return s.Repo.Delete(ctx, id, v.UserID, at, s.event(ctx, model.EventDeleted, at, map[string]any{
		"summaryId": doc.ID,
		"source":    string(doc.Source),
		"deletedBy": v.UserID,
		"deletedAt": at,
	}))
}

// moderationData son los datos de summary.approved y summary.rejected
// (ADR-005, 3.2). Los documentos de sistemas externos no generan mail ni
// beneficios porque su autor no tiene cuenta en Estudiario (RN-41): el evento
// lo dice en notifyAuthor y awardBenefits, para que los consumidores no
// tengan que deducirlo.
func moderationData(doc model.Summary, by string, at time.Time, to model.Status, reason string) map[string]any {
	student := doc.Source == model.SourceEstudiario
	author := map[string]any{"name": doc.Author.Name}
	if doc.Author.UserID != "" {
		author["id"] = doc.Author.UserID
	}
	data := map[string]any{
		"summaryId":   doc.ID,
		"title":       doc.Title,
		"subject":     doc.Category,
		"source":      string(doc.Source),
		"author":      author,
		"moderatedBy": by,
		"moderatedAt": at,
		// RN-41: sólo los resúmenes de estudiantes generan mail.
		"notifyAuthor": student,
	}
	if to == model.StatusApproved {
		// RN-41: sólo un resumen aprobado de un estudiante suma al programa de beneficios (RF-28).
		data["awardBenefits"] = student
	} else {
		data["reason"] = reason
	}
	return data
}

// event arma un evento con el sobre común del ADR-005 (3.2), sin publicar.
func (s *Service) event(ctx context.Context, eventType string, at time.Time, data map[string]any) model.OutboxEvent {
	return model.OutboxEvent{
		EventID:       s.newEventID(),
		EventType:     eventType,
		EventVersion:  1,
		OccurredAt:    at,
		Producer:      "forum-service",
		CorrelationID: correlation.ID(ctx),
		Data:          data,
	}
}
