// Package model tiene los tipos y errores que comparten las capas de
// forum-service (controller → service → repository, ADR-002).
//
// Un resumen del foro y un documento registrado por otro sistema (ADR-014) son
// lo mismo: un Summary. Cambia el origen (Source) y cómo se identifica al autor.
package model

import (
	"errors"
	"time"
)

// Status es el estado de moderación (SPEC 7.3). Los valores son los del contrato.
type Status string

const (
	StatusPending  Status = "pendiente"
	StatusApproved Status = "aprobado"
	StatusRejected Status = "rechazado"
	// StatusDeleted no se muestra nunca: el documento se conserva (con su outbox)
	// pero para todos es como si no existiera (RN-24, RN-39).
	StatusDeleted Status = "eliminado"
)

// Source dice quién registró el documento: "estudiario" si lo subió un
// estudiante en el foro, o el nombre del consumidor de la API key que lo
// registró por /v1 (por ejemplo "agencia_de_viajes", ADR-014).
type Source string

const (
	SourceEstudiario Source = "estudiario" // lo subió un estudiante en el foro
	// SourceExternal es el valor que tenían los documentos registrados por /v1
	// antes de que el source fuera el consumidor (ADR-014, actualización del
	// 2026-11-09). Ya no se guarda, pero los documentos viejos lo conservan.
	SourceExternal Source = "externo"
)

// Author identifica a quien subió el documento.
//   - Estudiante: UserID (el id de users-service), Name y Email, copiados del
//     token al publicar (ADR-003). Email es un dato personal: se guarda para
//     el mail de moderación (ADR-005) y nunca sale en las respuestas de la API.
//   - Sistema externo: Name, ExternalID (el id en su sistema, sin interpretar) y
//     Consumer, el consumidor de la API key que lo registró.
type Author struct {
	UserID     string `bson:"userId,omitempty"`
	Name       string `bson:"name"`
	Email      string `bson:"email,omitempty"`
	ExternalID string `bson:"externalId,omitempty"`
	Consumer   string `bson:"consumer,omitempty"`
}

// Rating es la calificación de los estudiantes (RN-31).
type Rating struct {
	Average float64 `bson:"average"` // 0 mientras Count sea 0
	Count   int     `bson:"count"`
}

// FileRef es la referencia al PDF guardado en MinIO (ADR-003).
type FileRef struct {
	Key         string `bson:"key"`
	ContentType string `bson:"contentType"`
	SizeBytes   int64  `bson:"sizeBytes"`
	FileName    string `bson:"fileName"`
	// Public indica que el PDF está en el bucket público (public-documents) y
	// no en summaries. Lo tienen los documentos de un source externo
	// registrados desde la actualización del ADR-014; los anteriores no.
	Public bool `bson:"public,omitempty"`
}

// OutboxEvent es un evento de dominio guardado en el mismo documento que el
// cambio de estado, con el formato del ADR-005 (3.2). Lo publica el relay a
// RabbitMQ y marca PublishedAt.
type OutboxEvent struct {
	EventID       string         `bson:"eventId"`
	EventType     string         `bson:"eventType"`
	EventVersion  int            `bson:"eventVersion"`
	OccurredAt    time.Time      `bson:"occurredAt"`
	Producer      string         `bson:"producer"`
	CorrelationID string         `bson:"correlationId,omitempty"`
	Data          map[string]any `bson:"data"`
	PublishedAt   *time.Time     `bson:"publishedAt"`
}

// Tipos de evento (ADR-005).
const (
	EventApproved = "summary.approved"
	EventRejected = "summary.rejected"
	EventDeleted  = "summary.deleted"
	// EventExternalRegistered avisa que un consumidor registró un documento con
	// el mail de su autor, para que users-service le cree una cuenta (ADR-014).
	EventExternalRegistered = "document.external-registered"
)

// Summary es un resumen del foro o un documento externo.
type Summary struct {
	ID          string `bson:"_id"`
	Title       string `bson:"title"`
	Description string `bson:"description"`
	// Category es la materia (foro) o el destino (externos). Texto libre (RN-25).
	Category string `bson:"category"`
	// CategoryKey es Category normalizada, para el filtro exacto sin mayúsculas.
	CategoryKey string   `bson:"categoryKey"`
	Tags        []string `bson:"tags"`
	// TagKeys son las etiquetas normalizadas, para filtrar sin mayúsculas.
	TagKeys         []string      `bson:"tagKeys"`
	Author          Author        `bson:"author"`
	Source          Source        `bson:"source"`
	Status          Status        `bson:"status"`
	RejectionReason string        `bson:"rejectionReason,omitempty"`
	Rating          Rating        `bson:"rating"`
	File            FileRef       `bson:"file"`
	CreatedAt       time.Time     `bson:"createdAt"`
	UpdatedAt       time.Time     `bson:"updatedAt"`
	PublishedAt     *time.Time    `bson:"publishedAt"`
	ModeratedBy     string        `bson:"moderatedBy,omitempty"`
	ModeratedAt     *time.Time    `bson:"moderatedAt,omitempty"`
	DeletedAt       *time.Time    `bson:"deletedAt,omitempty"`
	Outbox          []OutboxEvent `bson:"outbox"`
}

// Visible indica si el documento existe para alguien (no fue eliminado).
func (s Summary) Visible() bool { return s.Status != StatusDeleted }

// PublicFile indica si su PDF se entrega sin autenticación, por
// /v1/public/documents/{id}/file: es de un source externo, está en el bucket
// público y no fue rechazado ni eliminado (ADR-014).
func (s Summary) PublicFile() bool {
	return s.File.Public && s.Source != SourceEstudiario &&
		(s.Status == StatusPending || s.Status == StatusApproved)
}

var (
	ErrNotFound = errors.New("el resumen no existe")
	// ErrAlreadyModerated: se quiso aprobar o rechazar algo que ya no está pendiente (RN-21, RN-23).
	ErrAlreadyModerated = errors.New("la publicación ya fue moderada")
	// ErrNotApproved: se quiso calificar algo que no está aprobado (RN-28).
	ErrNotApproved = errors.New("sólo se pueden calificar publicaciones aprobadas")
	// ErrOwnSummary: un estudiante quiso calificar su propio resumen (RN-29).
	ErrOwnSummary = errors.New("no podés calificar tu propio resumen")
	// ErrForbidden: la operación es sólo para administradores.
	ErrForbidden = errors.New("la operación es sólo para administradores")
	// ErrNotAuthor: un estudiante quiso eliminar un resumen que no es suyo (RF-31).
	ErrNotAuthor = errors.New("sólo el autor puede eliminar su resumen")
	// ErrFileTooLarge y ErrUnsupportedFileType: el archivo no cumple RN-20.
	ErrFileTooLarge        = errors.New("el archivo pesa más de 10 MB")
	ErrUnsupportedFileType = errors.New("el archivo tiene que ser PDF")
	// ErrIdempotencyReused: misma Idempotency-Key con otro cuerpo.
	ErrIdempotencyReused = errors.New("la clave de idempotencia ya se usó con otro documento")
	// ErrRequestInProgress: hay un registro en curso con la misma Idempotency-Key.
	ErrRequestInProgress = errors.New("hay un registro en curso con la misma clave de idempotencia")
	// ErrUnavailable: MongoDB o MinIO no respondieron a tiempo o no están disponibles (ADR-010).
	ErrUnavailable = errors.New("el almacenamiento no está disponible")
	// ErrSearchUnavailable: la búsqueda no responde; el detalle y la descarga siguen (ADR-014).
	ErrSearchUnavailable = errors.New("la búsqueda no está disponible")
)

// FieldError es un problema de validación en un campo del pedido.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError agrupa los problemas de validación de un pedido.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return "hay campos inválidos" }
