package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// SearchTimeout es el tiempo máximo de una búsqueda (ADR-010: 2 s).
const SearchTimeout = 2 * time.Second

// MongoSearch es la implementación simple de la búsqueda de aprobados sobre
// MongoDB, con su índice de texto en español. Cumple la misma interfaz que va
// a cumplir la búsqueda sobre Solr (ADR-006), que la reemplaza más adelante.
type MongoSearch struct {
	summaries *mongo.Collection
	timeout   time.Duration
}

func NewSearch(db *mongo.Database) *MongoSearch {
	return &MongoSearch{summaries: db.Collection("summaries"), timeout: SearchTimeout}
}

// Search busca entre los aprobados (RN-24) con texto, filtros, orden y página
// (RF-19, RF-20). Si MongoDB no responde devuelve model.ErrSearchUnavailable.
func (s *MongoSearch) Search(ctx context.Context, q model.SearchQuery) (model.Page, error) {
	filter := bson.D{{Key: "status", Value: model.StatusApproved}}
	if q.Text != "" {
		filter = append(filter, bson.E{Key: "$text", Value: bson.D{{Key: "$search", Value: q.Text}}})
	}
	if q.Category != "" {
		filter = append(filter, bson.E{Key: "categoryKey", Value: q.Category})
	}
	if len(q.Tags) > 0 {
		filter = append(filter, bson.E{Key: "tagKeys", Value: bson.D{{Key: "$all", Value: q.Tags}}})
	}
	if q.MinRating > 0 {
		filter = append(filter, bson.E{Key: "rating.average", Value: bson.D{{Key: "$gte", Value: q.MinRating}}})
	}

	var sort bson.D
	switch q.Sort {
	case model.SortRating:
		sort = bson.D{{Key: "rating.average", Value: -1}, {Key: "rating.count", Value: -1}, {Key: "publishedAt", Value: -1}}
	case model.SortRelevance:
		if q.Text != "" {
			sort = bson.D{{Key: "score", Value: bson.D{{Key: "$meta", Value: "textScore"}}}, {Key: "publishedAt", Value: -1}}
			break
		}
		fallthrough
	default: // date
		sort = bson.D{{Key: "publishedAt", Value: -1}}
	}
	sort = append(sort, bson.E{Key: "_id", Value: 1}) // orden estable entre páginas

	page := model.Page{Page: q.Page, Size: q.Size, Items: []model.Summary{}}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	total, err := s.summaries.CountDocuments(ctx, filter)
	if err != nil {
		return page, searchErr(err)
	}
	page.Total = int(total)
	cur, err := s.summaries.Find(ctx, filter, options.Find().SetSort(sort).
		SetSkip(int64((q.Page-1)*q.Size)).SetLimit(int64(q.Size)))
	if err != nil {
		return page, searchErr(err)
	}
	if err := cur.All(ctx, &page.Items); err != nil {
		return page, searchErr(err)
	}
	return page, nil
}

func searchErr(err error) error {
	if errors.Is(classify(err), model.ErrUnavailable) {
		return errors.Join(model.ErrSearchUnavailable, err)
	}
	return err
}
