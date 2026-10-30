package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Lecturas que usa la búsqueda sobre Solr (internal/search, ADR-006).

// scanTimeout es el tiempo máximo para recorrer todos los aprobados en la
// reconciliación o la reconstrucción del índice.
const scanTimeout = 2 * time.Minute

// sinOutbox deja afuera los eventos: el índice no los necesita.
var sinOutbox = bson.D{{Key: "outbox", Value: 0}}

// GetMany devuelve los resúmenes con esos ids, en cualquier orden y estado.
// Los que no existen no vienen.
func (m *Mongo) GetMany(ctx context.Context, ids []string) (out []model.Summary, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		cur, err := m.summaries.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}},
			options.Find().SetProjection(sinOutbox))
		if err != nil {
			return err
		}
		return cur.All(ctx, &out)
	})
	return out, err
}

// ChangedSince devuelve los resúmenes con updatedAt desde since, en cualquier
// estado: el indexador agrega los aprobados y borra el resto.
func (m *Mongo) ChangedSince(ctx context.Context, since time.Time) (out []model.Summary, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		cur, err := m.summaries.Find(ctx, bson.D{{Key: "updatedAt", Value: bson.D{{Key: "$gte", Value: since}}}},
			options.Find().SetProjection(sinOutbox).SetSort(bson.D{{Key: "updatedAt", Value: 1}}))
		if err != nil {
			return err
		}
		return cur.All(ctx, &out)
	})
	return out, err
}

// EachApproved recorre todos los aprobados, de a uno, sin cargarlos todos en memoria.
func (m *Mongo) EachApproved(ctx context.Context, fn func(model.Summary) error) error {
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	cur, err := m.summaries.Find(ctx, bson.D{{Key: "status", Value: model.StatusApproved}},
		options.Find().SetProjection(sinOutbox).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return classify(err)
	}
	defer cur.Close(context.WithoutCancel(ctx))
	for cur.Next(ctx) {
		var s model.Summary
		if err := cur.Decode(&s); err != nil {
			return err
		}
		if err := fn(s); err != nil {
			return err
		}
	}
	return classify(cur.Err())
}
