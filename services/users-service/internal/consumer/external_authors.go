package consumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/mail"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
)

// AccountCreator crea la cuenta del autor externo (service.Accounts).
type AccountCreator interface {
	CreateExternalAuthor(ctx context.Context, in service.ExternalAuthor) error
}

// externalRegisteredData es data de document.external-registered, como lo
// publica forum-service (ADR-014, actualización del 2026-11-09).
type externalRegisteredData struct {
	DocumentID string `json:"documentId"`
	Title      string `json:"title"`
	Source     string `json:"source"`
	Author     struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
}

// ExternalAuthors consume document.external-registered de la cola
// users.external-authors: el autor de una guía que registró la agencia de
// viajes recibe una cuenta en Estudiario y su contraseña por mail.
type ExternalAuthors struct {
	Accounts AccountCreator
	Logger   *slog.Logger
	Delays   []time.Duration
	Jitter   func() float64
}

func (h *ExternalAuthors) Handle(ctx context.Context, msg Message) Decision {
	var data externalRegisteredData
	ev, err := parse(msg.Body, TypeDocumentExternalRegistered, &data)
	log := h.Logger.With("eventId", firstNonEmpty(ev.EventID, msg.MessageID), "eventType", ev.EventType,
		"correlationId", ev.CorrelationID, "documentId", data.DocumentID, "retries", msg.Retries)
	policy := retryPolicy{Delays: h.Delays, Jitter: h.Jitter}
	if err != nil {
		return policy.decide(log, err, msg.Retries)
	}
	if data.Author.Email == "" {
		log.Info("el documento no trae el mail del autor: no se crea cuenta")
		return Decision{Action: Ack}
	}
	err = h.Accounts.CreateExternalAuthor(ctx, service.ExternalAuthor{
		EventID: ev.EventID, Name: data.Author.Name, Email: data.Author.Email,
		Source: data.Source, DocumentTitle: data.Title,
	})
	var verr *model.ValidationError
	switch {
	case err == nil:
		return Decision{Action: Ack}
	case errors.As(err, &verr):
		return policy.decide(log, permanent("datos del autor inválidos: %v", verr.Fields), msg.Retries)
	case errors.Is(err, mail.ErrPermanent):
		return policy.decide(log, permanent("%v", err), msg.Retries)
	default:
		return policy.decide(log, err, msg.Retries)
	}
}
