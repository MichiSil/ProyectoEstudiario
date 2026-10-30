package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

type fakeOutbox struct {
	pending []model.PendingEvent
	marked  []string
	markErr error
}

func (f *fakeOutbox) UnpublishedEvents(context.Context, int) ([]model.PendingEvent, error) {
	var out []model.PendingEvent
	for _, p := range f.pending {
		if !f.isMarked(p.Event.EventID) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeOutbox) MarkPublished(_ context.Context, _, eventID string, _ time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = append(f.marked, eventID)
	return nil
}

func (f *fakeOutbox) isMarked(id string) bool {
	for _, m := range f.marked {
		if m == id {
			return true
		}
	}
	return false
}

type fakePublisher struct {
	published []string
	failOn    string // eventId que falla
}

func (f *fakePublisher) Publish(_ context.Context, ev model.OutboxEvent) error {
	if ev.EventID == f.failOn {
		return errors.New("broker caído")
	}
	f.published = append(f.published, ev.EventID)
	return nil
}

func pending(ids ...string) []model.PendingEvent {
	out := make([]model.PendingEvent, len(ids))
	for i, id := range ids {
		out[i] = model.PendingEvent{SummaryID: "s-" + id, Event: model.OutboxEvent{EventID: id, EventType: model.EventApproved}}
	}
	return out
}

func newRelay(o Outbox, p Publisher) *Relay {
	return &Relay{Outbox: o, Publisher: p, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestFlushPublicaEnOrdenYMarcaDespuesDeLaConfirmacion(t *testing.T) {
	out := &fakeOutbox{pending: pending("e1", "e2", "e3")}
	pub := &fakePublisher{}

	n, err := newRelay(out, pub).Flush(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("Flush = %d, %v; se esperaban 3 publicados sin error", n, err)
	}
	if got := pub.published; len(got) != 3 || got[0] != "e1" || got[2] != "e3" {
		t.Errorf("publicados = %v, se esperaba e1, e2, e3 en orden", got)
	}
	if len(out.marked) != 3 {
		t.Errorf("marcados = %v, se esperaban los 3", out.marked)
	}

	// Una segunda vuelta no encuentra nada pendiente.
	n, err = newRelay(out, pub).Flush(context.Background())
	if err != nil || n != 0 {
		t.Errorf("segunda vuelta = %d, %v; no debería publicar nada", n, err)
	}
}

func TestFlushConElBrokerCaidoNoMarcaYCorta(t *testing.T) {
	out := &fakeOutbox{pending: pending("e1", "e2", "e3")}
	pub := &fakePublisher{failOn: "e2"}

	n, err := newRelay(out, pub).Flush(context.Background())
	if err == nil {
		t.Fatal("se esperaba error con el broker caído")
	}
	if n != 1 || len(out.marked) != 1 || out.marked[0] != "e1" {
		t.Errorf("publicados = %d, marcados = %v; sólo e1 debería quedar marcado", n, out.marked)
	}
	// e3 no se adelanta a e2: queda para la próxima vuelta.
	for _, id := range pub.published {
		if id == "e3" {
			t.Error("e3 se publicó antes que e2")
		}
	}

	// Cuando vuelve el broker, se publican los que quedaron en el outbox.
	pub.failOn = ""
	if n, err := newRelay(out, pub).Flush(context.Background()); err != nil || n != 2 {
		t.Errorf("al volver el broker = %d, %v; se esperaban e2 y e3", n, err)
	}
}

func TestFlushSiNoSePuedeMarcarDevuelveError(t *testing.T) {
	out := &fakeOutbox{pending: pending("e1"), markErr: errors.New("MongoDB caído")}
	pub := &fakePublisher{}

	if _, err := newRelay(out, pub).Flush(context.Background()); err == nil {
		t.Fatal("se esperaba error al no poder marcar publishedAt")
	}
	// Ya se publicó: en la próxima vuelta se publica de nuevo (al menos una vez)
	// y el consumidor lo descarta por eventId.
	if len(pub.published) != 1 {
		t.Errorf("publicados = %v", pub.published)
	}
}

func TestBackoffExponencialHasta30Segundos(t *testing.T) {
	wait := time.Second
	var got []time.Duration
	for range 7 {
		wait = backoff(wait, time.Second)
		got = append(got, wait)
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("esperas = %v, se esperaba %v", got, want)
		}
	}
}

func TestEnvelopeConvierteLosTiposDeBSONaJSON(t *testing.T) {
	at := time.Date(2026, 10, 20, 14, 3, 11, 0, time.UTC)
	// Así vuelve el evento al leerlo de MongoDB: subdocumentos como bson.D y
	// fechas como bson.DateTime.
	ev := model.OutboxEvent{
		EventID: "6f1d2b9e", EventType: model.EventRejected, EventVersion: 1,
		OccurredAt: at, Producer: "forum-service", CorrelationID: "c-1",
		Data: map[string]any{
			"summaryId":   "s-1",
			"author":      bson.D{{Key: "id", Value: "u-1"}, {Key: "name", Value: "Ana"}, {Key: "email", Value: "ana@example.com"}},
			"moderatedAt": bson.NewDateTimeFromTime(at),
			"reason":      "incompleto",
		},
	}
	body, err := json.Marshal(Envelope(ev))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		EventID       string `json:"eventId"`
		EventType     string `json:"eventType"`
		EventVersion  int    `json:"eventVersion"`
		OccurredAt    string `json:"occurredAt"`
		CorrelationID string `json:"correlationId"`
		Data          struct {
			Author struct {
				Email string `json:"email"`
			} `json:"author"`
			ModeratedAt string `json:"moderatedAt"`
			Reason      string `json:"reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, body)
	}
	if got.EventID != "6f1d2b9e" || got.EventType != model.EventRejected || got.EventVersion != 1 || got.CorrelationID != "c-1" {
		t.Errorf("sobre = %+v", got)
	}
	if got.OccurredAt != "2026-10-20T14:03:11Z" || got.Data.ModeratedAt != "2026-10-20T14:03:11Z" {
		t.Errorf("fechas = %q y %q, se esperaba RFC 3339 en UTC", got.OccurredAt, got.Data.ModeratedAt)
	}
	if got.Data.Author.Email != "ana@example.com" || got.Data.Reason != "incompleto" {
		t.Errorf("data = %s", body)
	}
}
