// Package consumer decide qué hacer con cada evento de moderación: mandar el
// mail al autor exactamente una vez (RN-32), reintentarlo más tarde o mandarlo
// a la DLQ (ADR-005, 3.4 y 3.5). No sabe nada de RabbitMQ: recibe el mensaje y
// devuelve una decisión, que aplica el paquete broker.
package consumer

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/event"
)

// RetryDelays son las esperas entre reintentos ante un error transitorio
// (ADR-005, 3.5): 5 s, 15 s, 45 s, 2 min 15 s y 6 min 45 s, unos 10 minutos
// en total. Agotados, el mensaje va a la DLQ y queda como fallido (RN-34).
var RetryDelays = []time.Duration{
	5 * time.Second,
	15 * time.Second,
	45 * time.Second,
	2*time.Minute + 15*time.Second,
	6*time.Minute + 45*time.Second,
}

// SendTimeout es lo máximo que se espera al servidor de mail (ADR-005, 3.4).
const SendTimeout = 10 * time.Second

// Estados de un registro de sent_notifications (ADR-005, 3.4).
const (
	StatusProcessing = "procesando"
	StatusSent       = "enviado"
	StatusFailed     = "fallido"
)

// Record es lo que se guarda de cada evento antes de mandar el mail.
type Record struct {
	EventID       string
	EventType     string
	SummaryID     string
	To            string
	CorrelationID string
	At            time.Time
}

// Store guarda cada evento en MongoDB con índice único sobre eventId.
type Store interface {
	// Begin registra el evento como "procesando". Devuelve alreadySent = true
	// si ya estaba como "enviado": es un duplicado y no se manda otro mail.
	Begin(ctx context.Context, r Record) (alreadySent bool, err error)
	MarkSent(ctx context.Context, eventID string, attempts int, at time.Time) error
	// Release libera el registro después de un error transitorio al mandar,
	// para que el reintento lo pueda tomar.
	Release(ctx context.Context, eventID, reason string, attempts int, at time.Time) error
	// MarkFailed deja el registro como "fallido" con el motivo (RN-34). Si no
	// existía, lo crea.
	MarkFailed(ctx context.Context, r Record, reason string, attempts int) error
}

// Mail es un mail ya armado.
type Mail struct {
	MessageID string // el eventId: identifica el mail en el servidor
	To        string
	ToName    string
	Subject   string
	Body      string
}

// Mailer manda un mail. Sus errores son transitorios salvo que estén marcados
// con event.ErrPermanent (por ejemplo, SMTP 5xx: la dirección no existe).
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// Message es un mensaje recibido de la cola.
type Message struct {
	Body []byte
	// MessageID es el message_id de AMQP (el eventId), para registrar el
	// fallo aunque el cuerpo no se pueda leer.
	MessageID string
	// Retries es cuántas veces se reintentó (encabezado x-retry-count).
	Retries int
}

// Action es lo que el broker tiene que hacer con el mensaje.
type Action int

const (
	Ack        Action = iota // procesado (o descartado): se confirma
	Retry                    // a notifications.retry, que lo devuelve después de Delay
	DeadLetter               // a notifications.dlq, con Reason
)

// Decision es el resultado de procesar un mensaje.
type Decision struct {
	Action Action
	Delay  time.Duration // con Retry
	Reason string        // con Retry y DeadLetter
}

// Handler procesa los eventos de moderación.
type Handler struct {
	Store  Store
	Mailer Mailer
	Logger *slog.Logger
	// Delays reemplaza a RetryDelays (los tests usan esperas cortas).
	Delays []time.Duration
	Now    func() time.Time
	// Jitter devuelve un número en [0, 1); por defecto, al azar.
	Jitter func() float64
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *Handler) delays() []time.Duration {
	if h.Delays != nil {
		return h.Delays
	}
	return RetryDelays
}

// Handle procesa un mensaje:
//
//  1. un evento inválido es un error permanente: va a la DLQ;
//  2. los documentos de sistemas externos no generan mail (RN-41);
//  3. un evento sin el mail del autor no se puede notificar: DLQ y "fallido";
//  4. se registra el eventId antes de mandar; si ya estaba "enviado", es un
//     duplicado y no se manda otro mail (RN-32);
//  5. se manda el mail con un timeout de 10 s y se marca "enviado".
//
// Un error transitorio (servidor de mail o MongoDB caídos) se reintenta con
// las esperas de RetryDelays, sin frenar a los demás mensajes; agotados los
// reintentos, o ante un error permanente, va a la DLQ como "fallido" (RN-34).
func (h *Handler) Handle(ctx context.Context, msg Message) Decision {
	ev, err := event.Parse(msg.Body)
	log := h.Logger.With("eventId", firstNonEmpty(ev.EventID, msg.MessageID),
		"eventType", ev.EventType, "correlationId", ev.CorrelationID, "retries", msg.Retries)
	rec := Record{
		EventID: firstNonEmpty(ev.EventID, msg.MessageID), EventType: ev.EventType,
		SummaryID: ev.Data.SummaryID, To: ev.Data.Author.Email,
		CorrelationID: ev.CorrelationID, At: h.now(),
	}
	if err != nil {
		return h.fail(ctx, log, rec, err, msg.Retries)
	}
	if !ev.ShouldNotify() {
		log.InfoContext(ctx, "evento de un documento externo: no genera mail (RN-41)", "summaryId", ev.Data.SummaryID)
		return Decision{Action: Ack}
	}
	if ev.Data.Author.Email == "" {
		return h.fail(ctx, log, rec, event.Permanent("el evento no trae author.email: no se puede notificar al autor"), msg.Retries)
	}

	alreadySent, err := h.Store.Begin(ctx, rec)
	if err != nil {
		return h.retryOrFail(ctx, log, rec, err, msg.Retries)
	}
	if alreadySent {
		log.InfoContext(ctx, "evento duplicado: el mail ya se había enviado (RN-32)")
		return Decision{Action: Ack}
	}

	sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)
	err = h.Mailer.Send(sendCtx, Compose(ev))
	cancel()
	if err != nil {
		d := h.retryOrFail(ctx, log, rec, err, msg.Retries)
		if d.Action == Retry {
			if relErr := h.Store.Release(ctx, rec.EventID, d.Reason, msg.Retries+1, h.now()); relErr != nil {
				// El reintento lo va a encontrar tomado y lo va a volver a
				// reintentar hasta que venza la toma: se demora, no se pierde.
				log.WarnContext(ctx, "no se pudo liberar el registro para el reintento", "error", relErr.Error())
			}
		}
		return d
	}

	if err := h.Store.MarkSent(ctx, rec.EventID, msg.Retries+1, h.now()); err != nil {
		// El mail ya salió: reintentar mandaría otro. Se confirma el mensaje y
		// el registro queda "procesando" para revisarlo (ADR-005, 3.4).
		log.ErrorContext(ctx, "el mail se envió pero no se pudo marcar como enviado", "error", err.Error())
		return Decision{Action: Ack}
	}
	log.InfoContext(ctx, "mail enviado", "summaryId", ev.Data.SummaryID)
	return Decision{Action: Ack}
}

// retryOrFail reintenta un error transitorio, o lo da por fallido si es
// permanente o si ya se agotaron los reintentos.
func (h *Handler) retryOrFail(ctx context.Context, log *slog.Logger, rec Record, err error, retries int) Decision {
	delays := h.delays()
	if event.IsPermanent(err) || retries >= len(delays) {
		return h.fail(ctx, log, rec, err, retries)
	}
	delay := h.jitter(delays[retries])
	log.WarnContext(ctx, "error transitorio, se reintenta más tarde",
		"error", err.Error(), "retry", retries+1, "delayMs", delay.Milliseconds())
	return Decision{Action: Retry, Delay: delay, Reason: err.Error()}
}

// fail manda el mensaje a la DLQ y deja el registro como "fallido" (RN-34).
func (h *Handler) fail(ctx context.Context, log *slog.Logger, rec Record, err error, retries int) Decision {
	reason := err.Error()
	if !event.IsPermanent(err) {
		reason = "reintentos agotados: " + reason
	}
	if rec.EventID != "" {
		if markErr := h.Store.MarkFailed(ctx, rec, reason, retries+1); markErr != nil {
			// El mensaje igual queda en la DLQ con el motivo: no se pierde.
			log.ErrorContext(ctx, "no se pudo registrar el mail como fallido", "error", markErr.Error())
		}
	}
	log.ErrorContext(ctx, "mensaje enviado a la DLQ", "reason", reason)
	return Decision{Action: DeadLetter, Reason: reason}
}

// jitter aplica ± 20 % a la espera, para que los reintentos no lleguen todos juntos.
func (h *Handler) jitter(d time.Duration) time.Duration {
	r := rand.Float64
	if h.Jitter != nil {
		r = h.Jitter
	}
	return time.Duration(float64(d) * (0.8 + 0.4*r()))
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
