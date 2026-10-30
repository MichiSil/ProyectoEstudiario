package model

import "time"

// Sort es el orden de una búsqueda (RF-20).
type Sort string

const (
	SortRelevance Sort = "relevance"
	SortRating    Sort = "rating"
	SortDate      Sort = "date"
)

// MaxPageSize es el tamaño máximo de una página (RN-27).
const MaxPageSize = 50

// DefaultPageSize es el tamaño de página si no se indica otro.
const DefaultPageSize = 20

// SearchQuery es una búsqueda entre los resúmenes aprobados (RF-19, RF-20).
// Category y Tags ya vienen normalizados (ver NormalizeKey).
type SearchQuery struct {
	Text      string
	Category  string
	Tags      []string
	MinRating float64
	Sort      Sort
	Page      int
	Size      int
}

// Page es una página de resultados.
type Page struct {
	Items []Summary
	Page  int
	Size  int
	Total int
}

// Moderation es el cambio de estado de una moderación, con el evento que se
// guarda en el outbox en la misma actualización (ADR-005, 3.3).
type Moderation struct {
	To     Status // aprobado o rechazado
	Reason string // motivo del rechazo (RN-22)
	By     string // id del administrador
	At     time.Time
	Event  OutboxEvent
}

// IdempotencyRecord registra un POST /v1/documents por API key y clave (RN-40).
type IdempotencyRecord struct {
	ID          string    `bson:"_id"` // consumidor + ":" + clave
	RequestHash string    `bson:"requestHash"`
	DocumentID  string    `bson:"documentId,omitempty"`
	Done        bool      `bson:"done"`
	CreatedAt   time.Time `bson:"createdAt"`
}
