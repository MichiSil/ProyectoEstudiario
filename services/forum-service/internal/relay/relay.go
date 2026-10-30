// Package relay publica en RabbitMQ los eventos que la moderación deja en el
// outbox de cada resumen (ADR-005, 3.3):
//
//  1. cada Interval (1 s) busca en MongoDB los eventos sin publicar;
//  2. los publica en el exchange forum.events y espera la confirmación del broker;
//  3. recién entonces marca publishedAt.
//
// Si RabbitMQ está caído, los eventos quedan en el outbox y el relay reintenta
// con espera exponencial (1 s, 2 s, 4 s…, hasta 30 s). Corre aparte del
// servidor HTTP, así que la moderación nunca espera al broker (RN-33).
//
// Si el proceso se cae entre la confirmación y la marca, el evento se vuelve a
// publicar: la entrega es al menos una vez, y los consumidores descartan los
// duplicados por eventId.
package relay

import (
	"context"
	"log/slog"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Valores del ADR-005 (3.3).
const (
	DefaultInterval = time.Second
	maxWait         = 30 * time.Second
	batch           = 100
)

// Outbox es lo que el relay lee y marca en MongoDB.
type Outbox interface {
	UnpublishedEvents(ctx context.Context, limit int) ([]model.PendingEvent, error)
	MarkPublished(ctx context.Context, summaryID, eventID string, at time.Time) error
}

// Publisher publica un evento y vuelve cuando el broker lo confirmó.
type Publisher interface {
	Publish(ctx context.Context, ev model.OutboxEvent) error
}

// Relay mueve los eventos del outbox a RabbitMQ.
type Relay struct {
	Outbox    Outbox
	Publisher Publisher
	Logger    *slog.Logger
	Interval  time.Duration
	Now       func() time.Time
}

func (r *Relay) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// Run publica hasta que se cancele ctx.
func (r *Relay) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	wait := interval
	for {
		if !sleep(ctx, wait) {
			return
		}
		n, err := r.Flush(ctx)
		if err != nil {
			// Espera exponencial: 1 s, 2 s, 4 s… hasta 30 s. Los eventos siguen en el outbox.
			wait = backoff(wait, interval)
			r.Logger.Warn("no se pudieron publicar los eventos del outbox, reintentando",
				"published", n, "waitMs", wait.Milliseconds(), "error", err.Error())
			continue
		}
		wait = interval
	}
}

// backoff duplica la espera después de un error, de interval hasta maxWait.
func backoff(current, interval time.Duration) time.Duration {
	if current < interval {
		return interval
	}
	return min(current*2, maxWait)
}

// Flush publica los eventos pendientes en orden y devuelve cuántos publicó.
// Ante el primer error corta, para no adelantar eventos de un mismo resumen.
func (r *Relay) Flush(ctx context.Context) (int, error) {
	pending, err := r.Outbox.UnpublishedEvents(ctx, batch)
	if err != nil {
		return 0, err
	}
	published := 0
	for _, p := range pending {
		if err := r.Publisher.Publish(ctx, p.Event); err != nil {
			return published, err
		}
		if err := r.Outbox.MarkPublished(ctx, p.SummaryID, p.Event.EventID, r.now()); err != nil {
			// Ya está en el broker: si no se marca, se publica de nuevo y el
			// consumidor lo descarta por eventId.
			return published, err
		}
		published++
		r.Logger.InfoContext(ctx, "evento publicado",
			"eventId", p.Event.EventID, "eventType", p.Event.EventType,
			"summaryId", p.SummaryID, "correlationId", p.Event.CorrelationID)
	}
	return published, nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
