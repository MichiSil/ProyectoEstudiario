package broker_test

// Tests de integración contra RabbitMQ y MongoDB reales: el consumidor
// completo, con la cola de reintentos y la DLQ, y el registro en MongoDB. Se
// ejecutan sólo si están definidas NOTIFICATION_TEST_AMQP_URL y
// NOTIFICATION_TEST_MONGO_URI.
//
// Cada test declara sus propias colas (test.<algo>.*) y las borra al
// terminar, así no compite con el notification-service que esté corriendo.
// Por eso NOTIFICATION_TEST_AMQP_URL usa el administrador de RabbitMQ (sólo
// en local): el usuario del servicio no puede declarar colas.
//
//	docker run --rm --network estudiario_default -v "$PWD/services/notification-service:/src" -w /src \
//	  -e NOTIFICATION_TEST_AMQP_URL='amqp://rabbit_admin:rabbit-admin-local@rabbitmq:5672/' \
//	  -e NOTIFICATION_TEST_MONGO_URI='mongodb://notification_svc:notification-local@mongo:27017/notifications?authSource=notifications' \
//	  golang:1.26-alpine go test ./internal/broker/

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/broker"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/repository"
)

const testPrefix = "test-integracion-"

// countingMailer cuenta los mails y falla las primeras failFirst veces (o
// siempre, si failFirst < 0).
type countingMailer struct {
	mu        sync.Mutex
	sent      map[string]int
	calls     int
	failFirst int
}

func (m *countingMailer) Send(_ context.Context, mail consumer.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.failFirst < 0 || m.calls <= m.failFirst {
		return errors.New("dial tcp mailpit:1025: connection refused")
	}
	m.sent[mail.MessageID]++
	return nil
}

func (m *countingMailer) count(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent[id]
}

type env struct {
	ch                     *amqp.Channel
	queue, retryQueue, dlq string
	store                  *repository.Mongo
	mailer                 *countingMailer
}

func setup(t *testing.T, failFirst int) *env {
	t.Helper()
	amqpURL, mongoURI := os.Getenv("NOTIFICATION_TEST_AMQP_URL"), os.Getenv("NOTIFICATION_TEST_MONGO_URI")
	if amqpURL == "" || mongoURI == "" {
		t.Skip("NOTIFICATION_TEST_AMQP_URL o NOTIFICATION_TEST_MONGO_URI no definidas: se omiten los tests de integración")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("notifications")
	store := repository.New(db)
	if err := store.EnsureIndexes(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Collection(repository.Collection).DeleteMany(context.Background(),
			bson.D{{Key: "eventId", Value: bson.D{{Key: "$regex", Value: "^" + testPrefix}}}})
		_ = client.Disconnect(context.Background())
	})

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	// La misma topología del ADR-005, con nombres propios del test.
	base := "test." + rand.Text()
	e := &env{ch: ch, queue: base + ".main", retryQueue: base + ".retry", dlq: base + ".dlq", store: store,
		mailer: &countingMailer{sent: map[string]int{}, failFirst: failFirst}}
	for _, q := range []struct {
		name string
		args amqp.Table
	}{
		{e.queue, nil},
		{e.retryQueue, amqp.Table{"x-dead-letter-exchange": "", "x-dead-letter-routing-key": e.queue}},
		{e.dlq, nil},
	} {
		if _, err := ch.QueueDeclare(q.name, true, false, false, false, q.args); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, q := range []string{e.queue, e.retryQueue, e.dlq} {
			_, _ = ch.QueueDelete(q, false, false, false)
		}
	})

	handler := &consumer.Handler{
		Store: store, Mailer: e.mailer,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Esperas cortas: la política es la misma, pero el test no tarda 10 minutos.
		Delays: []time.Duration{200 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&broker.Consumer{URL: amqpURL, Handler: handler, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Queue: e.queue, RetryQueue: e.retryQueue, DLQ: e.dlq}).Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return e
}

func (e *env) publish(t *testing.T, id string, withEmail bool) {
	t.Helper()
	author := map[string]any{"id": "u-1", "name": "Ana Pérez"}
	if withEmail {
		author["email"] = "ana@example.com"
	}
	body, _ := json.Marshal(map[string]any{
		"eventId": id, "eventType": "summary.approved", "eventVersion": 1,
		"occurredAt": time.Now().UTC(), "producer": "forum-service", "correlationId": "c-" + id,
		"data": map[string]any{
			"summaryId": "s-1", "title": "Resumen de integrales dobles", "subject": "Análisis II",
			"author": author, "notifyAuthor": true,
		},
	})
	err := e.ch.PublishWithContext(context.Background(), "", e.queue, false, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: id, Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// eventually reintenta cond hasta que se cumpla o pase el plazo.
func eventually(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("no se cumplió a tiempo: %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (e *env) status(id string) string {
	n, err := e.store.Get(context.Background(), id)
	if err != nil {
		return ""
	}
	return n.Status
}

// fromDLQ saca de la DLQ el mensaje con ese message_id.
func (e *env) fromDLQ(t *testing.T, id string) (amqp.Delivery, bool) {
	t.Helper()
	d, ok, err := e.ch.Get(e.dlq, true)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || d.MessageId != id {
		return amqp.Delivery{}, false
	}
	return d, true
}

// RN-32: el mismo evento publicado dos veces genera un único mail.
func TestEventoDuplicadoMandaUnSoloMail(t *testing.T) {
	e := setup(t, 0)
	id := testPrefix + rand.Text()

	e.publish(t, id, true)
	e.publish(t, id, true)

	eventually(t, 10*time.Second, "registro enviado", func() bool { return e.status(id) == consumer.StatusSent })
	time.Sleep(time.Second) // margen para que la segunda copia se procese
	if n := e.mailer.count(id); n != 1 {
		t.Fatalf("mails = %d, se esperaba 1", n)
	}
	if _, ok := e.fromDLQ(t, id); ok {
		t.Error("el duplicado no debería ir a la DLQ")
	}
}

// CA-26: con el mail caído, el mensaje pasa por la cola de reintentos y, al
// volver, llega una sola vez.
func TestErrorTransitorioPasaPorLaColaDeReintentos(t *testing.T) {
	e := setup(t, 2) // falla dos veces y después anda
	id := testPrefix + rand.Text()

	e.publish(t, id, true)

	eventually(t, 15*time.Second, "mail enviado después de reintentar", func() bool { return e.mailer.count(id) == 1 })
	eventually(t, 5*time.Second, "registro enviado", func() bool { return e.status(id) == consumer.StatusSent })
	n, _ := e.store.Get(context.Background(), id)
	if n.Attempts != 3 {
		t.Errorf("intentos = %d, se esperaban 3", n.Attempts)
	}
}

// CA-27: agotados los reintentos, va a la DLQ y queda como fallido.
func TestReintentosAgotadosVanALaDLQ(t *testing.T) {
	e := setup(t, -1) // el mail no anda nunca
	id := testPrefix + rand.Text()

	e.publish(t, id, true)

	eventually(t, 15*time.Second, "registro fallido", func() bool { return e.status(id) == consumer.StatusFailed })
	var d amqp.Delivery
	eventually(t, 5*time.Second, "mensaje en la DLQ", func() bool {
		var ok bool
		d, ok = e.fromDLQ(t, id)
		return ok
	})
	if got := d.Headers[broker.HeaderRetryCount]; got != int32(3) {
		t.Errorf("%s = %v, se esperaba 3", broker.HeaderRetryCount, got)
	}
	if reason, _ := d.Headers[broker.HeaderReason].(string); reason == "" {
		t.Error("la DLQ tiene que conservar el motivo")
	}
	if n := e.mailer.count(id); n != 0 {
		t.Errorf("mails = %d", n)
	}
}

// Un evento sin author.email es un error permanente: DLQ sin reintentos.
func TestErrorPermanenteVaDirectoALaDLQ(t *testing.T) {
	e := setup(t, 0)
	id := testPrefix + rand.Text()

	e.publish(t, id, false)

	var d amqp.Delivery
	eventually(t, 10*time.Second, "mensaje en la DLQ", func() bool {
		var ok bool
		d, ok = e.fromDLQ(t, id)
		return ok
	})
	if got := d.Headers[broker.HeaderRetryCount]; got != int32(0) {
		t.Errorf("%s = %v, se esperaba 0 (sin reintentos)", broker.HeaderRetryCount, got)
	}
	if e.status(id) != consumer.StatusFailed {
		t.Errorf("estado = %q, se esperaba fallido", e.status(id))
	}
	e.mailer.mu.Lock()
	calls := e.mailer.calls
	e.mailer.mu.Unlock()
	if calls != 0 {
		t.Errorf("no debería intentar mandar el mail")
	}
}
