package repository_test

// Tests de integración contra un MongoDB real. Se ejecutan sólo si está
// definida NOTIFICATION_TEST_MONGO_URI.
//
// Los puertos de MongoDB no se publican fuera de Docker: se corren en un
// contenedor conectado a la red de docker compose (ver el README):
//
//	docker run --rm --network estudiario_default -v "$PWD/services/notification-service:/src" -w /src \
//	  -e NOTIFICATION_TEST_MONGO_URI='mongodb://notification_svc:notification-local@mongo:27017/notifications?authSource=notifications' \
//	  golang:1.26-alpine go test ./internal/repository/

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/repository"
)

// testPrefix marca los registros de prueba, que se borran al terminar.
const testPrefix = "test-integracion-"

func newStore(t *testing.T) (*repository.Mongo, *mongo.Collection) {
	t.Helper()
	uri := os.Getenv("NOTIFICATION_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("NOTIFICATION_TEST_MONGO_URI no definida: se omiten los tests de integración con MongoDB")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	db := client.Database("notifications")
	store := repository.New(db)
	if err := store.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("índices: %v", err)
	}
	coll := db.Collection(repository.Collection)
	t.Cleanup(func() {
		_, _ = coll.DeleteMany(context.Background(), bson.D{{Key: "eventId", Value: bson.D{{Key: "$regex", Value: "^" + testPrefix}}}})
	})
	return store, coll
}

func record() consumer.Record {
	return consumer.Record{
		EventID: testPrefix + rand.Text(), EventType: "summary.approved", SummaryID: "s-1",
		To: "ana@example.com", CorrelationID: "c-1", At: time.Now().UTC(),
	}
}

func TestBeginDespuesDeEnviadoEsDuplicado(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	r := record()

	sent, err := store.Begin(ctx, r)
	if err != nil || sent {
		t.Fatalf("primer Begin = %v, %v; se esperaba tomarlo", sent, err)
	}
	if err := store.MarkSent(ctx, r.EventID, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	sent, err = store.Begin(ctx, r)
	if err != nil || !sent {
		t.Fatalf("Begin después de enviado = %v, %v; se esperaba alreadySent (RN-32)", sent, err)
	}
	n, err := store.Get(ctx, r.EventID)
	if err != nil || n.Status != consumer.StatusSent || n.SentAt.IsZero() {
		t.Errorf("registro = %+v, %v", n, err)
	}
}

func TestIndiceUnicoSobreEventID(t *testing.T) {
	_, coll := newStore(t)
	id := testPrefix + rand.Text()
	ctx := context.Background()
	if _, err := coll.InsertOne(ctx, bson.D{{Key: "eventId", Value: id}}); err != nil {
		t.Fatal(err)
	}
	_, err := coll.InsertOne(ctx, bson.D{{Key: "eventId", Value: id}})
	if !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("segunda inserción = %v, se esperaba error de clave duplicada", err)
	}
}

// Dos copias del mismo evento a la vez (entrega al menos una vez): sólo una
// toma el registro; las otras reciben ErrInProgress y se reintentan.
func TestCopiasALaVezSoloUnaTomaElRegistro(t *testing.T) {
	store, _ := newStore(t)
	r := record()

	const copies = 8
	var wg sync.WaitGroup
	results := make(chan error, copies)
	for range copies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sent, err := store.Begin(context.Background(), r)
			if sent {
				err = errors.New("alreadySent inesperado")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	took, inProgress := 0, 0
	for err := range results {
		switch {
		case err == nil:
			took++
		case errors.Is(err, repository.ErrInProgress):
			inProgress++
		default:
			t.Errorf("error inesperado: %v", err)
		}
	}
	if took != 1 || inProgress != copies-1 {
		t.Fatalf("lo tomaron %d y %d quedaron en curso; se esperaba 1 y %d", took, inProgress, copies-1)
	}
}

func TestReleasePermiteElReintento(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	r := record()

	if _, err := store.Begin(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(ctx, r); !errors.Is(err, repository.ErrInProgress) {
		t.Fatalf("Begin con el registro tomado = %v, se esperaba ErrInProgress", err)
	}
	if err := store.Release(ctx, r.EventID, "connection refused", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if sent, err := store.Begin(ctx, r); err != nil || sent {
		t.Fatalf("Begin después de liberar = %v, %v; se esperaba tomarlo", sent, err)
	}
}

func TestTomaVencidaSeRecupera(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	r := record()
	r.At = time.Now().UTC().Add(-2 * repository.Lease) // un proceso que se cayó hace rato

	if _, err := store.Begin(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.At = time.Now().UTC()
	if sent, err := store.Begin(ctx, r); err != nil || sent {
		t.Fatalf("Begin con la toma vencida = %v, %v; se esperaba tomarlo", sent, err)
	}
}

// RN-34: queda "fallido"; si se reinyecta desde la DLQ, se vuelve a intentar.
func TestFallidoSeRegistraYSePuedeReintentar(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	r := record()

	// Falló antes de registrarse (por ejemplo, sin author.email): se crea.
	if err := store.MarkFailed(ctx, r, "reintentos agotados: connection refused", 6); err != nil {
		t.Fatal(err)
	}
	n, err := store.Get(ctx, r.EventID)
	if err != nil || n.Status != consumer.StatusFailed || n.Attempts != 6 || n.Error == "" || n.To != r.To {
		t.Fatalf("registro = %+v, %v", n, err)
	}
	if sent, err := store.Begin(ctx, r); err != nil || sent {
		t.Fatalf("Begin de un fallido = %v, %v; se esperaba tomarlo de nuevo", sent, err)
	}
}

func TestMarkFailedNoPisaUnEnviado(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	r := record()

	if _, err := store.Begin(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSent(ctx, r.EventID, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailed(ctx, r, "tarde", 2); err != nil {
		t.Fatalf("MarkFailed = %v", err)
	}
	if n, _ := store.Get(ctx, r.EventID); n.Status != consumer.StatusSent {
		t.Errorf("estado = %q, se esperaba que siga enviado", n.Status)
	}
}
