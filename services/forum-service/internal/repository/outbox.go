package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// UnpublishedEvents devuelve hasta limit eventos del outbox que todavía no se
// publicaron, en el orden en que se guardaron dentro de cada resumen
// (ADR-005, 3.3). Usa el índice sobre outbox.publishedAt.
func (m *Mongo) UnpublishedEvents(ctx context.Context, limit int) (events []model.PendingEvent, err error) {
	err = m.run(ctx, func(ctx context.Context) error {
		cur, err := m.summaries.Find(ctx,
			bson.D{{Key: "outbox", Value: bson.D{{Key: "$elemMatch", Value: bson.D{{Key: "publishedAt", Value: nil}}}}}},
			options.Find().SetProjection(bson.D{{Key: "outbox", Value: 1}}).SetLimit(int64(limit)))
		if err != nil {
			return err
		}
		var docs []model.Summary
		if err := cur.All(ctx, &docs); err != nil {
			return err
		}
		for _, d := range docs {
			for _, ev := range d.Outbox {
				if ev.PublishedAt == nil && len(events) < limit {
					events = append(events, model.PendingEvent{SummaryID: d.ID, Event: ev})
				}
			}
		}
		return nil
	})
	return events, err
}

// MarkPublished marca un evento del outbox como publicado. No toca updatedAt:
// publicar no es un cambio del resumen.
func (m *Mongo) MarkPublished(ctx context.Context, summaryID, eventID string, at time.Time) error {
	return m.run(ctx, func(ctx context.Context) error {
		_, err := m.summaries.UpdateOne(ctx,
			bson.D{{Key: "_id", Value: summaryID}, {Key: "outbox.eventId", Value: eventID}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "outbox.$.publishedAt", Value: at}}}})
		return err
	})
}
