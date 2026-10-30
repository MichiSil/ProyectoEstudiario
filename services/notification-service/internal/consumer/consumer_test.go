package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/event"
)

// fakeStore se comporta como sent_notifications con índice único sobre eventId.
type fakeStore struct {
	status   map[string]string
	reasons  map[string]string
	beginErr error
}

func newStore() *fakeStore {
	return &fakeStore{status: map[string]string{}, reasons: map[string]string{}}
}

func (s *fakeStore) Begin(_ context.Context, r consumer.Record) (bool, error) {
	if s.beginErr != nil {
		return false, s.beginErr
	}
	if s.status[r.EventID] == consumer.StatusSent {
		return true, nil
	}
	s.status[r.EventID] = consumer.StatusProcessing
	return false, nil
}

func (s *fakeStore) MarkSent(_ context.Context, id string, _ int, _ time.Time) error {
	s.status[id] = consumer.StatusSent
	return nil
}

func (s *fakeStore) Release(context.Context, string, string, int, time.Time) error { return nil }

func (s *fakeStore) MarkFailed(_ context.Context, r consumer.Record, reason string, _ int) error {
	s.status[r.EventID] = consumer.StatusFailed
	s.reasons[r.EventID] = reason
	return nil
}

type fakeMailer struct {
	sent []consumer.Mail
	err  error
}

func (m *fakeMailer) Send(ctx context.Context, mail consumer.Mail) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("el envío tiene que tener timeout")
	}
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, mail)
	return nil
}

func newHandler(s consumer.Store, m consumer.Mailer) *consumer.Handler {
	return &consumer.Handler{
		Store: s, Mailer: m,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Jitter: func() float64 { return 0.5 }, // sin variación: la espera exacta
	}
}

func body(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	ev := map[string]any{
		"eventId":       "6f1d2b9e-3c4a-4e8b-9f0a-1b2c3d4e5f60",
		"eventType":     "summary.rejected",
		"eventVersion":  1,
		"occurredAt":    "2026-10-20T14:03:11Z",
		"producer":      "forum-service",
		"correlationId": "c0a8012e-7f3b",
		"data": map[string]any{
			"summaryId":    "6650f1c2a9e4b3d2c1a0f9e8",
			"title":        "Resumen de integrales dobles",
			"subject":      "Análisis Matemático II",
			"source":       "estudiario",
			"author":       map[string]any{"id": "u-123", "name": "Ana Pérez", "email": "ana@example.com"},
			"moderatedBy":  "u-001",
			"moderatedAt":  "2026-10-20T14:03:11Z",
			"reason":       "El PDF está incompleto: faltan las páginas 3 a 5.",
			"notifyAuthor": true,
		},
	}
	if mutate != nil {
		mutate(ev)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func data(ev map[string]any) map[string]any { return ev["data"].(map[string]any) }

const eventID = "6f1d2b9e-3c4a-4e8b-9f0a-1b2c3d4e5f60"

func TestEnviaElMailYLoMarcaEnviado(t *testing.T) {
	store, mailer := newStore(), &fakeMailer{}

	d := newHandler(store, mailer).Handle(context.Background(), consumer.Message{Body: body(t, nil)})

	if d.Action != consumer.Ack {
		t.Fatalf("decisión = %+v, se esperaba Ack", d)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mails = %d, se esperaba 1", len(mailer.sent))
	}
	m := mailer.sent[0]
	if m.To != "ana@example.com" || m.MessageID != eventID || !strings.Contains(m.Subject, "rechazado") {
		t.Errorf("mail = %+v", m)
	}
	// RF-24: el mail del rechazo incluye el motivo.
	if !strings.Contains(m.Body, "faltan las páginas 3 a 5") {
		t.Errorf("el mail no incluye el motivo:\n%s", m.Body)
	}
	if store.status[eventID] != consumer.StatusSent {
		t.Errorf("estado = %q, se esperaba enviado", store.status[eventID])
	}
}

func TestMailDeAprobacion(t *testing.T) {
	mailer := &fakeMailer{}
	newHandler(newStore(), mailer).Handle(context.Background(), consumer.Message{Body: body(t, func(ev map[string]any) {
		ev["eventType"] = "summary.approved"
		delete(data(ev), "reason")
	})})
	if len(mailer.sent) != 1 || mailer.sent[0].Subject != "Tu resumen fue publicado" {
		t.Fatalf("mails = %+v, se esperaba el de publicado (RF-23)", mailer.sent)
	}
	if !strings.Contains(mailer.sent[0].Body, "Resumen de integrales dobles") {
		t.Errorf("el mail no nombra el resumen:\n%s", mailer.sent[0].Body)
	}
}

// RN-32: el mismo evento entregado dos veces manda un único mail.
func TestDuplicadoNoMandaOtroMail(t *testing.T) {
	store, mailer := newStore(), &fakeMailer{}
	h := newHandler(store, mailer)

	first := h.Handle(context.Background(), consumer.Message{Body: body(t, nil)})
	second := h.Handle(context.Background(), consumer.Message{Body: body(t, nil)})

	if first.Action != consumer.Ack || second.Action != consumer.Ack {
		t.Fatalf("decisiones = %+v y %+v, se esperaban dos Ack", first, second)
	}
	if len(mailer.sent) != 1 {
		t.Errorf("mails = %d, se esperaba 1 solo", len(mailer.sent))
	}
}

// RN-33 y RN-34: con el servidor de mail caído se reintenta con las esperas
// del ADR-005, sin marcarlo como fallido mientras queden reintentos.
func TestErrorTransitorioSeReintentaConBackoff(t *testing.T) {
	store, mailer := newStore(), &fakeMailer{err: errors.New("dial tcp mailpit:1025: connection refused")}
	h := newHandler(store, mailer)

	for retries, want := range consumer.RetryDelays {
		d := h.Handle(context.Background(), consumer.Message{Body: body(t, nil), Retries: retries})
		if d.Action != consumer.Retry {
			t.Fatalf("intento %d: decisión = %+v, se esperaba Retry", retries+1, d)
		}
		if d.Delay != want {
			t.Errorf("intento %d: espera = %v, se esperaba %v", retries+1, d.Delay, want)
		}
		if store.status[eventID] == consumer.StatusFailed {
			t.Fatalf("intento %d: quedó fallido con reintentos pendientes", retries+1)
		}
	}

	// Cuando vuelve el servidor, el reintento manda el mail una sola vez (CA-26).
	mailer.err = nil
	d := h.Handle(context.Background(), consumer.Message{Body: body(t, nil), Retries: 3})
	if d.Action != consumer.Ack || len(mailer.sent) != 1 || store.status[eventID] != consumer.StatusSent {
		t.Fatalf("al volver el mail: decisión = %+v, mails = %d, estado = %q", d, len(mailer.sent), store.status[eventID])
	}
}

func TestEsperaConVariacionDeMasMenos20PorCiento(t *testing.T) {
	for _, j := range []struct {
		r    float64
		want time.Duration
	}{{0, 4 * time.Second}, {0.999999, 6 * time.Second}} {
		h := newHandler(newStore(), &fakeMailer{err: errors.New("timeout")})
		h.Jitter = func() float64 { return j.r }
		d := h.Handle(context.Background(), consumer.Message{Body: body(t, nil)})
		if diff := d.Delay - j.want; diff < -time.Millisecond || diff > time.Millisecond {
			t.Errorf("jitter %v: espera = %v, se esperaba ~%v", j.r, d.Delay, j.want)
		}
	}
}

// CA-27: agotados los reintentos, va a la DLQ y queda como fallido.
func TestReintentosAgotadosVaALaDLQComoFallido(t *testing.T) {
	store, mailer := newStore(), &fakeMailer{err: errors.New("connection refused")}

	d := newHandler(store, mailer).Handle(context.Background(),
		consumer.Message{Body: body(t, nil), Retries: len(consumer.RetryDelays)})

	if d.Action != consumer.DeadLetter {
		t.Fatalf("decisión = %+v, se esperaba DeadLetter", d)
	}
	if store.status[eventID] != consumer.StatusFailed || !strings.Contains(store.reasons[eventID], "reintentos agotados") {
		t.Errorf("registro = %q (%q), se esperaba fallido por reintentos agotados", store.status[eventID], store.reasons[eventID])
	}
}

// Un error permanente del servidor (SMTP 5xx) no se reintenta.
func TestErrorPermanenteDelMailVaDirectoALaDLQ(t *testing.T) {
	store := newStore()
	mailer := &fakeMailer{err: event.Permanent("RCPT TO: el servidor de mail respondió 550 no existe")}

	d := newHandler(store, mailer).Handle(context.Background(), consumer.Message{Body: body(t, nil)})

	if d.Action != consumer.DeadLetter {
		t.Fatalf("decisión = %+v, se esperaba DeadLetter sin reintentos", d)
	}
	if store.status[eventID] != consumer.StatusFailed {
		t.Errorf("estado = %q, se esperaba fallido", store.status[eventID])
	}
}

func TestEventosInvalidosSonPermanentes(t *testing.T) {
	cases := map[string][]byte{
		"JSON inválido":         []byte(`{"eventId":`),
		"sin eventId":           body(t, func(ev map[string]any) { delete(ev, "eventId") }),
		"eventType desconocido": body(t, func(ev map[string]any) { ev["eventType"] = "summary.deleted" }),
		"versión desconocida":   body(t, func(ev map[string]any) { ev["eventVersion"] = 2 }),
		"rechazo sin motivo":    body(t, func(ev map[string]any) { delete(data(ev), "reason") }),
		// El autor no tiene mail (por ejemplo, un token sin email): no se puede notificar.
		"sin author.email": body(t, func(ev map[string]any) { delete(data(ev)["author"].(map[string]any), "email") }),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			store, mailer := newStore(), &fakeMailer{}
			d := newHandler(store, mailer).Handle(context.Background(), consumer.Message{Body: b, MessageID: "msg-1"})
			if d.Action != consumer.DeadLetter {
				t.Fatalf("decisión = %+v, se esperaba DeadLetter", d)
			}
			if len(mailer.sent) != 0 {
				t.Error("no debería mandar mail")
			}
		})
	}
}

func TestSinAuthorEmailQuedaFallido(t *testing.T) {
	store := newStore()
	newHandler(store, &fakeMailer{}).Handle(context.Background(), consumer.Message{Body: body(t, func(ev map[string]any) {
		delete(data(ev)["author"].(map[string]any), "email")
	})})
	if store.status[eventID] != consumer.StatusFailed || !strings.Contains(store.reasons[eventID], "author.email") {
		t.Errorf("registro = %q (%q)", store.status[eventID], store.reasons[eventID])
	}
}

// RN-41: los documentos de sistemas externos no generan mail.
func TestDocumentoExternoNoGeneraMail(t *testing.T) {
	store, mailer := newStore(), &fakeMailer{}
	d := newHandler(store, mailer).Handle(context.Background(), consumer.Message{Body: body(t, func(ev map[string]any) {
		data(ev)["notifyAuthor"] = false
		data(ev)["source"] = "externo"
		data(ev)["author"] = map[string]any{"name": "Cátedra de Física"}
	})})
	if d.Action != consumer.Ack || len(mailer.sent) != 0 {
		t.Fatalf("decisión = %+v, mails = %d; se esperaba Ack sin mail", d, len(mailer.sent))
	}
	if _, ok := store.status[eventID]; ok {
		t.Error("no debería registrar nada")
	}
}

// Si MongoDB no responde, no se sabe si ya se mandó: se reintenta sin mandar.
func TestMongoCaidoSeReintentaSinMandar(t *testing.T) {
	store, mailer := newStore(), &fakeMailer{}
	store.beginErr = errors.New("MongoDB: server selection timeout")

	d := newHandler(store, mailer).Handle(context.Background(), consumer.Message{Body: body(t, nil)})

	if d.Action != consumer.Retry || len(mailer.sent) != 0 {
		t.Fatalf("decisión = %+v, mails = %d; se esperaba Retry sin mail", d, len(mailer.sent))
	}
}
