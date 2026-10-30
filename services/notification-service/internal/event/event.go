// Package event lee los eventos de moderación que publica forum-service en el
// exchange forum.events, con el sobre común del ADR-005 (3.2).
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tipos de evento que consume notification-service.
const (
	TypeApproved = "summary.approved"
	TypeRejected = "summary.rejected"
)

// Version es la única versión del formato que se entiende (ADR-005, 3.2).
const Version = 1

// ErrPermanent marca un error que no se arregla reintentando: el mensaje va
// directo a la DLQ (ADR-005, 3.5). Los demás errores son transitorios.
var ErrPermanent = errors.New("error permanente")

// Permanent envuelve un error como permanente.
func Permanent(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrPermanent, fmt.Sprintf(format, a...))
}

// IsPermanent dice si err no se arregla reintentando.
func IsPermanent(err error) bool { return errors.Is(err, ErrPermanent) }

// Author es el autor del resumen, copiado por forum-service al publicar. Los
// documentos de sistemas externos no traen id ni email (RN-41).
type Author struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Data son los datos de summary.approved y summary.rejected.
type Data struct {
	SummaryID   string    `json:"summaryId"`
	Title       string    `json:"title"`
	Subject     string    `json:"subject"`
	Source      string    `json:"source"`
	Author      Author    `json:"author"`
	ModeratedBy string    `json:"moderatedBy"`
	ModeratedAt time.Time `json:"moderatedAt"`
	Reason      string    `json:"reason"`
	// NotifyAuthor es false para los documentos de sistemas externos (RN-41).
	// Si no viene (eventos anteriores a ese campo), se notifica.
	NotifyAuthor *bool `json:"notifyAuthor"`
}

// Event es el sobre común de los eventos (ADR-005, 3.2).
type Event struct {
	EventID       string    `json:"eventId"`
	EventType     string    `json:"eventType"`
	EventVersion  int       `json:"eventVersion"`
	OccurredAt    time.Time `json:"occurredAt"`
	Producer      string    `json:"producer"`
	CorrelationID string    `json:"correlationId"`
	Data          Data      `json:"data"`
}

// ShouldNotify dice si el evento genera mail: no lo generan los documentos de
// sistemas externos, porque su autor no tiene cuenta en Estudiario (RN-41).
func (e Event) ShouldNotify() bool {
	return e.Data.NotifyAuthor == nil || *e.Data.NotifyAuthor
}

// Parse lee y valida un evento. Todos sus errores son permanentes: un mensaje
// mal formado no se arregla reintentando. Los campos desconocidos se ignoran,
// porque agregar campos no cambia la versión (ADR-005, 3.2).
func Parse(body []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(body, &e); err != nil {
		return Event{}, Permanent("JSON inválido: %v", err)
	}
	if strings.TrimSpace(e.EventID) == "" {
		return e, Permanent("falta eventId")
	}
	if e.EventType != TypeApproved && e.EventType != TypeRejected {
		return e, Permanent("eventType desconocido: %q", e.EventType)
	}
	if e.EventVersion != Version {
		return e, Permanent("eventVersion desconocida: %d", e.EventVersion)
	}
	if e.Data.SummaryID == "" || e.Data.Title == "" {
		return e, Permanent("faltan data.summaryId o data.title")
	}
	if e.EventType == TypeRejected && strings.TrimSpace(e.Data.Reason) == "" {
		return e, Permanent("un summary.rejected tiene que traer data.reason")
	}
	return e, nil
}
