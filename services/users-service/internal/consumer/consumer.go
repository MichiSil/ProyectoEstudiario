// Package consumer decide qué hacer con cada evento que consume users-service:
// procesarlo, reintentarlo más tarde o mandarlo a la DLQ (ADR-005, 3.4 y 3.5).
// No sabe nada de RabbitMQ: recibe el mensaje y devuelve una decisión, que
// aplica el paquete broker, igual que en notification-service.
package consumer

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
)

// RetryDelays son las esperas entre reintentos ante un error transitorio
// (ADR-005, 3.5): 5 s, 15 s, 45 s, 2 min 15 s y 6 min 45 s. Agotados, el
// mensaje va a la DLQ.
var RetryDelays = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	45 * time.Second,
	2*time.Minute + 15*time.Second,
	6*time.Minute + 45*time.Second,
}

// Tipos de evento que consume users-service, publicados por forum-service en
// el exchange forum.events.
const (
	TypeSummaryApproved            = "summary.approved"
	TypeDocumentExternalRegistered = "document.external-registered"
)

// Version es la única versión del sobre que se entiende (ADR-005, 3.2).
const Version = 1

// ErrPermanent marca un error que no se arregla reintentando: el mensaje va
// directo a la DLQ.
var ErrPermanent = errors.New("error permanente")

func permanent(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrPermanent, fmt.Sprintf(format, a...))
}

// Message es un mensaje recibido de la cola.
type Message struct {
	Body      []byte
	MessageID string
	Retries   int // encabezado x-retry-count
}

// Action es lo que el broker tiene que hacer con el mensaje.
type Action int

const (
	Ack        Action = iota // procesado (o descartado): se confirma
	Retry                    // a la cola de reintentos, que lo devuelve después de Delay
	DeadLetter               // a la DLQ, con Reason
)

// Decision es el resultado de procesar un mensaje.
type Decision struct {
	Action Action
	Delay  time.Duration
	Reason string
}

// Event es el sobre común de los eventos (ADR-005, 3.2). Data se lee después,
// según el tipo. Los campos desconocidos se ignoran.
type Event struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	EventVersion  int             `json:"eventVersion"`
	OccurredAt    time.Time       `json:"occurredAt"`
	CorrelationID string          `json:"correlationId"`
	Data          json.RawMessage `json:"data"`
}

// parse lee el sobre y valida que sea del tipo esperado. Todos sus errores son
// permanentes: un mensaje mal formado no se arregla reintentando.
func parse(body []byte, wantType string, data any) (Event, error) {
	var e Event
	if err := json.Unmarshal(body, &e); err != nil {
		return e, permanent("JSON inválido: %v", err)
	}
	switch {
	case strings.TrimSpace(e.EventID) == "":
		return e, permanent("falta eventId")
	case e.EventType != wantType:
		return e, permanent("eventType inesperado: %q", e.EventType)
	case e.EventVersion != Version:
		return e, permanent("eventVersion desconocida: %d", e.EventVersion)
	}
	if err := json.Unmarshal(e.Data, data); err != nil {
		return e, permanent("data inválida: %v", err)
	}
	return e, nil
}

// retryPolicy arma la decisión ante un error: DLQ si es permanente o se
// agotaron los reintentos, Retry con la espera que corresponde si no.
type retryPolicy struct {
	Delays []time.Duration
	Jitter func() float64
}

func (p retryPolicy) decide(log *slog.Logger, err error, retries int) Decision {
	delays := p.Delays
	if delays == nil {
		delays = RetryDelays
	}
	if errors.Is(err, ErrPermanent) {
		log.Error("evento descartado a la DLQ", "reason", err.Error())
		return Decision{Action: DeadLetter, Reason: err.Error()}
	}
	if retries >= len(delays) {
		log.Error("se agotaron los reintentos, el evento va a la DLQ", "reason", err.Error())
		return Decision{Action: DeadLetter, Reason: "reintentos agotados: " + err.Error()}
	}
	jitter := rand.Float64
	if p.Jitter != nil {
		jitter = p.Jitter
	}
	d := delays[retries]
	d += time.Duration(float64(d) * 0.2 * (jitter()*2 - 1)) // ±20 %
	log.Warn("error transitorio, el evento se reintenta", "delayMs", d.Milliseconds(), "reason", err.Error())
	return Decision{Action: Retry, Delay: d, Reason: err.Error()}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
