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
	EventID       string    `bson:"eventId"`
	EventType     string    `bson:"eventType,omitempty"`
	SummaryID     string    `bson:"summaryId,omitempty"`
	To            string    `bson:"to,omitempty"`
	CorrelationID string    `bson:"correlationId,omitempty"`
	Status        string    `bson:"status"`
	Attempts      int       `bson:"attempts"`
	Error         string    `bson:"error,omitempty"`
	CreatedAt     time.Time `bson:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt"`
	SentAt        time.Time `bson:"sentAt,omitzero"`
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

// Begin inserta el registro como "procesando". Si el eventId ya existe
// (índice único), es una reentrega: si ya estaba "enviado" devuelve
// alreadySent; si no (quedó "procesando" o "fallido" y se reinyectó desde la
// DLQ), lo vuelve a poner "procesando" para intentar de nuevo.
func (m *Mongo) Begin(ctx context.Context, r consumer.Record) (alreadySent bool, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		_, err := m.coll.InsertOne(ctx, Notification{
			EventID: r.EventID, EventType: r.EventType, SummaryID: r.SummaryID, To: r.To,
			CorrelationID: r.CorrelationID, Status: consumer.StatusProcessing,
			CreatedAt: r.At, UpdatedAt: r.At,
		})
		if err == nil || !mongo.IsDuplicateKeyError(err) {
			return err
		}
		res, err := m.coll.UpdateOne(ctx,
			bson.D{{Key: "eventId", Value: r.EventID}, {Key: "status", Value: bson.D{{Key: "$ne", Value: consumer.StatusSent}}}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: consumer.StatusProcessing}, {Key: "updatedAt", Value: r.At}}}})
		if err != nil {
			return err
		}
		alreadySent = res.MatchedCount == 0
		return nil
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
					{Key: "sentAt", Value: at}, {Key: "updatedAt", Value: at},
				}},
				{Key: "$unset", Value: bson.D{{Key: "error", Value: ""}}},
			})
		return err
	})
}

// MarkFailed deja el registro como "fallido" con el motivo (RN-34), o lo crea
// si el mensaje falló antes de registrarse. Nunca pisa uno "enviado".
func (m *Mongo) MarkFailed(ctx context.Context, r consumer.Record, reason string, attempts int) error {
	return m.run(ctx, func(ctx context.Context) error {
		set := bson.D{
			{Key: "status", Value: consumer.StatusFailed}, {Key: "error", Value: reason},
			{Key: "attempts", Value: attempts}, {Key: "updatedAt", Value: r.At},
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
