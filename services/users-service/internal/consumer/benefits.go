package consumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
)

// Awarder registra los puntos por un resumen aprobado (service.Club).
type Awarder interface {
	AwardApprovedSummary(ctx context.Context, s service.ApprovedSummary) error
}

// approvedData son los campos de summary.approved que se usan.
type approvedData struct {
	SummaryID string `json:"summaryId"`
	Author    struct {
		ID string `json:"id"`
	} `json:"author"`
	// AwardBenefits lo pone forum-service: false para los documentos de
	// sistemas externos (RN-41). Si no viene, se decide por el autor.
	AwardBenefits *bool `json:"awardBenefits"`
}

// Benefits consume summary.approved de la cola users.benefits: cada resumen
// aprobado de un estudiante le suma puntos en el Club de Beneficios (RF-28).
//
//   - Los puntos se guardan en la base antes de llamar al Club, con el
//     eventId del hecho, así el mismo resumen suma una sola vez aunque el
//     evento llegue repetido (RN-42).
//   - Si el Club no responde, el mensaje se reintenta con espera. Si se agotan
//     los reintentos va a la DLQ, pero los puntos ya están guardados como
//     pendientes y el barrido los acredita cuando el Club vuelva (RN-43).
type Benefits struct {
	Club   Awarder
	Logger *slog.Logger
	Delays []time.Duration // reemplaza a RetryDelays (los tests usan esperas cortas)
	Jitter func() float64
}

func (h *Benefits) Handle(ctx context.Context, msg Message) Decision {
	var data approvedData
	ev, err := parse(msg.Body, TypeSummaryApproved, &data)
	log := h.Logger.With("eventId", firstNonEmpty(ev.EventID, msg.MessageID), "eventType", ev.EventType,
		"correlationId", ev.CorrelationID, "summaryId", data.SummaryID, "retries", msg.Retries)
	policy := retryPolicy{Delays: h.Delays, Jitter: h.Jitter}
	if err != nil {
		return policy.decide(log, err, msg.Retries)
	}
	if data.SummaryID == "" {
		return policy.decide(log, permanent("falta data.summaryId"), msg.Retries)
	}
	if (data.AwardBenefits != nil && !*data.AwardBenefits) || data.Author.ID == "" {
		log.Info("el resumen no suma puntos: no es de un estudiante de Estudiario (RN-41)")
		return Decision{Action: Ack}
	}

	err = h.Club.AwardApprovedSummary(ctx, service.ApprovedSummary{SummaryID: data.SummaryID, AuthorID: data.Author.ID})
	switch {
	case err == nil:
		return Decision{Action: Ack}
	case errors.Is(err, model.ErrNotFound):
		return policy.decide(log, permanent("el autor %s no existe en users-service", data.Author.ID), msg.Retries)
	case errors.Is(err, club.ErrMisconfigured):
		// La API key del Club falla: los puntos quedan pendientes y el barrido
		// los acredita cuando se corrija la configuración. Reintentar no sirve.
		log.Error("el Club rechazó la API key: los puntos quedan pendientes", "error", err.Error())
		return Decision{Action: Ack}
	case errors.Is(err, club.ErrNotLinked):
		// El Club dice que la cuenta no está vinculada: los puntos quedan
		// pendientes hasta que la vinculación se complete.
		log.Warn("el Club no reconoce la cuenta: los puntos quedan pendientes", "error", err.Error())
		return Decision{Action: Ack}
	default:
		return policy.decide(log, err, msg.Retries)
	}
}
