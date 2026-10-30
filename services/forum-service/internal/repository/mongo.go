// Package repository es la capa de datos de forum-service sobre MongoDB
// (base `forum`, ADR-003).
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/topology"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// DefaultTimeout es el tiempo máximo de cada operación contra MongoDB (ADR-010).
const DefaultTimeout = 3 * time.Second

// IdempotencyTTL es cuánto se recuerda una Idempotency-Key (ADR-014: 24 h).
const IdempotencyTTL = 24 * time.Hour

// Mongo guarda los resúmenes, las calificaciones y los registros de
// idempotencia. Cada operación tiene un tiempo máximo y, si MongoDB no
// responde, devuelve model.ErrUnavailable, que la API informa como 503.
type Mongo struct {
	db          *mongo.Database
	summaries   *mongo.Collection
	ratings     *mongo.Collection
	idempotency *mongo.Collection
	timeout     time.Duration
}

func New(db *mongo.Database) *Mongo {
	return &Mongo{
		db:          db,
		summaries:   db.Collection("summaries"),
		ratings:     db.Collection("ratings"),
		idempotency: db.Collection("idempotency"),
		timeout:     DefaultTimeout,
	}
}

// EnsureIndexes crea los índices. Es idempotente.
func (m *Mongo) EnsureIndexes(ctx context.Context) error {
	return m.run(ctx, func(ctx context.Context) error {
		if _, err := m.summaries.Indexes().CreateMany(ctx, []mongo.IndexModel{
			// Pendientes de la más vieja a la más nueva (RF-16).
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: 1}}},
			// Listado de aprobados por fecha de publicación.
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "publishedAt", Value: -1}}},
			// Mis publicaciones (RF-15) y lo registrado por cada API key (RF-27).
			{Keys: bson.D{{Key: "author.userId", Value: 1}, {Key: "createdAt", Value: -1}}},
			{Keys: bson.D{{Key: "author.consumer", Value: 1}, {Key: "createdAt", Value: -1}}},
			// Eventos sin publicar, para el relay del outbox (ADR-005, 3.3).
			{Keys: bson.D{{Key: "outbox.publishedAt", Value: 1}}},
			// Cambios recientes, para el indexador de Solr (ADR-006).
			{Keys: bson.D{{Key: "updatedAt", Value: 1}}},
			// Búsqueda por texto en español, con más peso en el título.
			{
				Keys: bson.D{{Key: "title", Value: "text"}, {Key: "description", Value: "text"}, {Key: "category", Value: "text"}},
				Options: options.Index().SetName("texto").SetDefaultLanguage("spanish").
					SetWeights(bson.D{{Key: "title", Value: 5}, {Key: "category", Value: 2}, {Key: "description", Value: 1}}),
			},
		}); err != nil {
			return err
		}
		// Una calificación por estudiante y resumen (RN-30).
		if _, err := m.ratings.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: "summaryId", Value: 1}, {Key: "userId", Value: 1}},
			Options: options.Index().SetUnique(true),
		}); err != nil {
			return err
		}
		// Las claves de idempotencia se borran solas después de 24 h.
		_, err := m.idempotency.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: "createdAt", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(int32(IdempotencyTTL.Seconds())),
		})
		return err
	})
}

func (m *Mongo) Ping(ctx context.Context) error {
	return m.run(ctx, func(ctx context.Context) error {
		return m.db.Client().Ping(ctx, nil)
	})
}

// ---------- Resúmenes ----------

func (m *Mongo) Create(ctx context.Context, s model.Summary) error {
	if s.Outbox == nil {
		s.Outbox = []model.OutboxEvent{}
	}
	if s.Tags == nil {
		s.Tags, s.TagKeys = []string{}, []string{}
	}
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.summaries.InsertOne(ctx, s)
		return err
	})
}

// Get devuelve el resumen en cualquier estado, incluido eliminado: la capa de
// servicio decide quién puede verlo.
func (m *Mongo) Get(ctx context.Context, id string) (s model.Summary, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		err := m.summaries.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&s)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.ErrNotFound
		}
		return err
	})
	return s, err
}

// ListByUser devuelve las publicaciones de un estudiante, de la más nueva a la
// más vieja, sin las eliminadas (RF-15).
func (m *Mongo) ListByUser(ctx context.Context, userID string, page, size int) (model.Page, error) {
	return m.list(ctx, bson.D{
		{Key: "author.userId", Value: userID},
		{Key: "status", Value: bson.D{{Key: "$ne", Value: model.StatusDeleted}}},
	}, bson.D{{Key: "createdAt", Value: -1}}, page, size)
}

// ListByConsumer devuelve lo registrado con una API key, de lo más nuevo a lo
// más viejo, sin lo eliminado (RF-27). status vacío trae todos los estados.
func (m *Mongo) ListByConsumer(ctx context.Context, consumer string, status model.Status, page, size int) (model.Page, error) {
	filter := bson.D{{Key: "author.consumer", Value: consumer}}
	if status != "" {
		filter = append(filter, bson.E{Key: "status", Value: status})
	} else {
		filter = append(filter, bson.E{Key: "status", Value: bson.D{{Key: "$ne", Value: model.StatusDeleted}}})
	}
	return m.list(ctx, filter, bson.D{{Key: "createdAt", Value: -1}}, page, size)
}

// ListPending devuelve las pendientes de la más vieja a la más nueva (RF-16).
func (m *Mongo) ListPending(ctx context.Context, page, size int) (model.Page, error) {
	return m.list(ctx, bson.D{{Key: "status", Value: model.StatusPending}},
		bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}, page, size)
}

func (m *Mongo) list(ctx context.Context, filter, sort bson.D, page, size int) (p model.Page, err error) {
	p = model.Page{Page: page, Size: size, Items: []model.Summary{}}
	err = m.run(ctx, func(ctx context.Context) error {
		total, err := m.summaries.CountDocuments(ctx, filter)
		if err != nil {
			return err
		}
		p.Total = int(total)
		cur, err := m.summaries.Find(ctx, filter, options.Find().SetSort(sort).
			SetSkip(int64((page-1)*size)).SetLimit(int64(size)))
		if err != nil {
			return err
		}
		return cur.All(ctx, &p.Items)
	})
	return p, err
}

// Moderate aprueba o rechaza un resumen pendiente con UNA actualización
// condicional: cambia el estado sólo si sigue pendiente y, en la misma
// operación, agrega el evento al outbox (ADR-005, 3.3). Si dos administradores
// moderan a la vez, sólo una actualización encuentra el documento pendiente;
// la otra recibe model.ErrAlreadyModerated (RN-21, RN-23).
func (m *Mongo) Moderate(ctx context.Context, id string, mod model.Moderation) error {
	set := bson.D{
		{Key: "status", Value: mod.To},
		{Key: "moderatedBy", Value: mod.By},
		{Key: "moderatedAt", Value: mod.At},
		{Key: "updatedAt", Value: mod.At},
	}
	switch mod.To {
	case model.StatusApproved:
		set = append(set, bson.E{Key: "publishedAt", Value: mod.At})
	case model.StatusRejected:
		set = append(set, bson.E{Key: "rejectionReason", Value: mod.Reason})
	default:
		return fmt.Errorf("estado de moderación inválido: %q", mod.To)
	}
	return m.run(ctx, func(ctx context.Context) error {
		res, err := m.summaries.UpdateOne(ctx,
			bson.D{{Key: "_id", Value: id}, {Key: "status", Value: model.StatusPending}},
			bson.D{{Key: "$set", Value: set}, {Key: "$push", Value: bson.D{{Key: "outbox", Value: mod.Event}}}})
		if err != nil {
			return err
		}
		if res.MatchedCount == 1 {
			return nil
		}
		return m.whyNotMatched(ctx, id)
	})
}

// Delete elimina un resumen en cualquier estado (RF-18) y agrega el evento al
// outbox en la misma actualización. El documento se conserva marcado como
// eliminado: deja de verse en todos lados (RN-24, RN-39).
func (m *Mongo) Delete(ctx context.Context, id, by string, at time.Time, event model.OutboxEvent) error {
	return m.run(ctx, func(ctx context.Context) error {
		res, err := m.summaries.UpdateOne(ctx,
			bson.D{{Key: "_id", Value: id}, {Key: "status", Value: bson.D{{Key: "$ne", Value: model.StatusDeleted}}}},
			bson.D{
				{Key: "$set", Value: bson.D{
					{Key: "status", Value: model.StatusDeleted},
					{Key: "deletedAt", Value: at},
					{Key: "moderatedBy", Value: by},
					{Key: "updatedAt", Value: at},
				}},
				{Key: "$push", Value: bson.D{{Key: "outbox", Value: event}}},
			})
		if err != nil {
			return err
		}
		if res.MatchedCount == 0 {
			return model.ErrNotFound
		}
		return nil
	})
}

// whyNotMatched distingue, después de una actualización condicional que no
// encontró el documento, si no existe (o fue eliminado) o si ya se moderó.
func (m *Mongo) whyNotMatched(ctx context.Context, id string) error {
	var s model.Summary
	err := m.summaries.FindOne(ctx, bson.D{{Key: "_id", Value: id}},
		options.FindOne().SetProjection(bson.D{{Key: "status", Value: 1}})).Decode(&s)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return model.ErrNotFound
	case err != nil:
		return err
	case s.Status == model.StatusDeleted:
		return model.ErrNotFound
	default:
		return model.ErrAlreadyModerated
	}
}

// ---------- Calificaciones ----------

// Rate guarda la calificación de un estudiante: si ya había calificado, se
// reemplaza su puntaje (RN-30, índice único). Después recalcula el promedio y
// la cantidad del resumen (RN-31). Las dos escrituras no están en una
// transacción: el promedio puede quedar desfasado un instante (ADR-003).
func (m *Mongo) Rate(ctx context.Context, summaryID, userID string, score int, at time.Time) (r model.Rating, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		_, err := m.ratings.UpdateOne(ctx,
			bson.D{{Key: "summaryId", Value: summaryID}, {Key: "userId", Value: userID}},
			bson.D{
				{Key: "$set", Value: bson.D{{Key: "score", Value: score}, {Key: "updatedAt", Value: at}}},
				{Key: "$setOnInsert", Value: bson.D{{Key: "createdAt", Value: at}}},
			},
			options.UpdateOne().SetUpsert(true))
		if mongo.IsDuplicateKeyError(err) {
			// Dos upserts simultáneos del mismo estudiante: el otro ya insertó; se reintenta como actualización.
			_, err = m.ratings.UpdateOne(ctx,
				bson.D{{Key: "summaryId", Value: summaryID}, {Key: "userId", Value: userID}},
				bson.D{{Key: "$set", Value: bson.D{{Key: "score", Value: score}, {Key: "updatedAt", Value: at}}}})
		}
		if err != nil {
			return err
		}
		cur, err := m.ratings.Aggregate(ctx, mongo.Pipeline{
			{{Key: "$match", Value: bson.D{{Key: "summaryId", Value: summaryID}}}},
			{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "average", Value: bson.D{{Key: "$avg", Value: "$score"}}},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			}}},
		})
		if err != nil {
			return err
		}
		var out []model.Rating
		if err := cur.All(ctx, &out); err != nil {
			return err
		}
		if len(out) == 1 {
			r = out[0]
		}
		_, err = m.summaries.UpdateOne(ctx, bson.D{{Key: "_id", Value: summaryID}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "rating", Value: r}}}})
		return err
	})
	return r, err
}

// ---------- Idempotencia (RN-40) ----------

// ReserveIdempotency intenta registrar la clave. Si ya existía, devuelve el
// registro existente y reserved = false.
func (m *Mongo) ReserveIdempotency(ctx context.Context, rec model.IdempotencyRecord) (existing model.IdempotencyRecord, reserved bool, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		_, err := m.idempotency.InsertOne(ctx, rec)
		if err == nil {
			reserved = true
			return nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			return err
		}
		return m.idempotency.FindOne(ctx, bson.D{{Key: "_id", Value: rec.ID}}).Decode(&existing)
	})
	return existing, reserved, err
}

// ClaimStaleIdempotency toma una reserva que quedó en curso desde antes de
// olderThan (el pedido original se cortó sin terminar). Devuelve false si otro
// pedido la tomó o la completó primero.
func (m *Mongo) ClaimStaleIdempotency(ctx context.Context, rec model.IdempotencyRecord, olderThan time.Time) (claimed bool, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		res, err := m.idempotency.ReplaceOne(ctx,
			bson.D{{Key: "_id", Value: rec.ID}, {Key: "done", Value: false}, {Key: "createdAt", Value: bson.D{{Key: "$lt", Value: olderThan}}}},
			rec)
		claimed = err == nil && res.MatchedCount == 1
		return err
	})
	return claimed, err
}

func (m *Mongo) CompleteIdempotency(ctx context.Context, id, documentID string) error {
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.idempotency.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "done", Value: true}, {Key: "documentId", Value: documentID}}}})
		return err
	})
}

// ReleaseIdempotency borra una reserva que no se completó, para que el
// consumidor pueda reintentar con la misma clave.
func (m *Mongo) ReleaseIdempotency(ctx context.Context, id string) error {
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.idempotency.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "done", Value: false}})
		return err
	})
}

// ---------- Tiempos y errores ----------

func (m *Mongo) run(ctx context.Context, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	return classify(fn(ctx))
}

// classify traduce los errores de disponibilidad de MongoDB a
// model.ErrUnavailable (503) y deja pasar los demás.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var selErr topology.ServerSelectionError
	if errors.Is(err, context.DeadlineExceeded) || mongo.IsTimeout(err) || mongo.IsNetworkError(err) || errors.As(err, &selErr) {
		return fmt.Errorf("%w: MongoDB: %v", model.ErrUnavailable, err)
	}
	return err
}
