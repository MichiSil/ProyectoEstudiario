// Package repository guarda en MongoDB (base `notifications`, ADR-003) un
// registro por evento de moderación, para mandar cada mail una sola vez
// (ADR-005, 3.4).
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
)

// DefaultTimeout es el tiempo máximo de cada operación contra MongoDB (ADR-010).
const DefaultTimeout = 3 * time.Second

// Collection es la colección de los registros (ADR-005, 3.4).
const Collection = "sent_notifications"

// Notification es un documento de sent_notifications.
type Notification struct {
	EventID       string `bson:"eventId"`
	EventType     string `bson:"eventType,omitempty"`
	SummaryID     string `bson:"summaryId,omitempty"`
	To            string `bson:"to,omitempty"`
	CorrelationID string `bson:"correlationId,omitempty"`
	Status        string `bson:"status"`
	Attempts      int    `bson:"attempts"`
	Error         string `bson:"error,omitempty"`
	// LockedUntil: mientras no venza, un proceso está mandando el mail y otra
	// copia del evento no lo toma. Al reintentar o fallar se libera.
	LockedUntil time.Time `bson:"lockedUntil"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
	SentAt      time.Time `bson:"sentAt,omitzero"`
}

// Mongo implementa consumer.Store.
type Mongo struct {
	coll    *mongo.Collection
	timeout time.Duration
}

func New(db *mongo.Database) *Mongo {
	return &Mongo{coll: db.Collection(Collection), timeout: DefaultTimeout}
}

// EnsureIndexes crea el índice único sobre eventId, que es lo que impide
// mandar dos veces el mismo mail (RN-32), y uno por estado para revisar los
// fallidos (RN-34). Es idempotente.
func (m *Mongo) EnsureIndexes(ctx context.Context) error {
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
			{Keys: bson.D{{Key: "eventId", Value: 1}}, Options: options.Index().SetUnique(true)},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "updatedAt", Value: -1}}},
		})
		return err
	})
}

func (m *Mongo) Ping(ctx context.Context) error {
	return m.run(ctx, func(ctx context.Context) error {
		return m.coll.Database().Client().Ping(ctx, nil)
	})
}

// Lease es cuánto queda tomado un registro mientras se manda el mail: más que
// el timeout del mail (10 s) y que las operaciones contra MongoDB. Pasado ese
// tiempo, se asume que el proceso que lo tomó se cayó.
const Lease = time.Minute

// ErrInProgress: otra copia del mismo evento se está procesando ahora. Es
// transitorio: el mensaje se reintenta y, para entonces, ya figura "enviado".
var ErrInProgress = errors.New("otra copia del evento se está procesando")

// Begin inserta el registro como "procesando" y lo toma por Lease. Si el
// eventId ya existe (índice único), es una reentrega:
//   - "enviado": devuelve alreadySent, no se manda otro mail (RN-32);
//   - libre (esperando un reintento, "fallido" y reinyectado desde la DLQ, o
//     tomado por un proceso que se cayó hace más de Lease): lo toma;
//   - tomado: otra copia llegó a la vez (entrega al menos una vez); devuelve
//     ErrInProgress, para no mandar dos mails en paralelo.
func (m *Mongo) Begin(ctx context.Context, r consumer.Record) (alreadySent bool, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		_, err := m.coll.InsertOne(ctx, Notification{
			EventID: r.EventID, EventType: r.EventType, SummaryID: r.SummaryID, To: r.To,
			CorrelationID: r.CorrelationID, Status: consumer.StatusProcessing,
			LockedUntil: r.At.Add(Lease), CreatedAt: r.At, UpdatedAt: r.At,
		})
		if err == nil || !mongo.IsDuplicateKeyError(err) {
			return err
		}
		res, err := m.coll.UpdateOne(ctx,
			bson.D{
				{Key: "eventId", Value: r.EventID},
				{Key: "status", Value: bson.D{{Key: "$ne", Value: consumer.StatusSent}}},
				{Key: "lockedUntil", Value: bson.D{{Key: "$lt", Value: r.At}}},
			},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "status", Value: consumer.StatusProcessing},
				{Key: "lockedUntil", Value: r.At.Add(Lease)},
				{Key: "updatedAt", Value: r.At},
			}}})
		if err != nil {
			return err
		}
		if res.MatchedCount == 1 {
			return nil
		}
		var existing Notification
		if err := m.coll.FindOne(ctx, bson.D{{Key: "eventId", Value: r.EventID}}).Decode(&existing); err != nil {
			return err
		}
		if existing.Status == consumer.StatusSent {
			alreadySent = true
			return nil
		}
		return ErrInProgress
	})
	return alreadySent, err
}

// MarkSent deja el registro como "enviado".
func (m *Mongo) MarkSent(ctx context.Context, eventID string, attempts int, at time.Time) error {
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.coll.UpdateOne(ctx, bson.D{{Key: "eventId", Value: eventID}},
			bson.D{
				{Key: "$set", Value: bson.D{
					{Key: "status", Value: consumer.StatusSent}, {Key: "attempts", Value: attempts},
					{Key: "sentAt", Value: at}, {Key: "lockedUntil", Value: time.Time{}},
					{Key: "updatedAt", Value: at},
				}},
				{Key: "$unset", Value: bson.D{{Key: "error", Value: ""}}},
			})
		return err
	})
}

// Release libera el registro después de un error transitorio, para que el
// reintento lo pueda tomar. Sigue "procesando", con el error y los intentos.
func (m *Mongo) Release(ctx context.Context, eventID, reason string, attempts int, at time.Time) error {
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.coll.UpdateOne(ctx,
			bson.D{{Key: "eventId", Value: eventID}, {Key: "status", Value: consumer.StatusProcessing}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "lockedUntil", Value: time.Time{}}, {Key: "error", Value: reason},
				{Key: "attempts", Value: attempts}, {Key: "updatedAt", Value: at},
			}}})
		return err
	})
}

// MarkFailed deja el registro como "fallido" con el motivo (RN-34), o lo crea
// si el mensaje falló antes de registrarse. Nunca pisa uno "enviado".
func (m *Mongo) MarkFailed(ctx context.Context, r consumer.Record, reason string, attempts int) error {
	return m.run(ctx, func(ctx context.Context) error {
		set := bson.D{
			{Key: "status", Value: consumer.StatusFailed}, {Key: "error", Value: reason},
			{Key: "attempts", Value: attempts}, {Key: "lockedUntil", Value: time.Time{}},
			{Key: "updatedAt", Value: r.At},
		}
		onInsert := bson.D{{Key: "createdAt", Value: r.At}}
		for _, f := range []struct{ key, value string }{
			{"eventType", r.EventType}, {"summaryId", r.SummaryID}, {"to", r.To}, {"correlationId", r.CorrelationID},
		} {
			if f.value != "" {
				onInsert = append(onInsert, bson.E{Key: f.key, Value: f.value})
			}
		}
		_, err := m.coll.UpdateOne(ctx,
			bson.D{{Key: "eventId", Value: r.EventID}, {Key: "status", Value: bson.D{{Key: "$ne", Value: consumer.StatusSent}}}},
			bson.D{{Key: "$set", Value: set}, {Key: "$setOnInsert", Value: onInsert}},
			options.UpdateOne().SetUpsert(true))
		if mongo.IsDuplicateKeyError(err) {
			return nil // ya estaba "enviado": no se pisa
		}
		return err
	})
}

// Get devuelve el registro de un evento (lo usan los tests).
func (m *Mongo) Get(ctx context.Context, eventID string) (n Notification, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		return m.coll.FindOne(ctx, bson.D{{Key: "eventId", Value: eventID}}).Decode(&n)
	})
	return n, err
}

func (m *Mongo) run(ctx context.Context, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	if err := fn(ctx); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		return fmt.Errorf("MongoDB: %w", err)
	}
	return nil
}
