// Package controller es la capa HTTP de forum-service (ADR-002): lee el
// pedido, llama al service y arma la respuesta. Tiene dos APIs:
//
//   - la del foro (forum.go), para el frontend a través del api-gateway;
//   - la capacidad publicada /v1 (v1.go), para otros sistemas (ADR-014).
//
// Las dos devuelven el documento con la misma forma, la del contrato
// docs/contracts/documents-v1.yaml.
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/correlation"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
)

// Pinger permite que /health verifique MongoDB.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health responde GET /health: 503 si MongoDB no responde (contrato documents-v1).
func Health(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status, code := "ok", http.StatusOK
		if err := db.Ping(ctx); err != nil {
			status, code = "degraded", http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]string{"status": status, "service": "forum-service"})
	}
}

// ---------- Documento (forma del contrato) ----------

type authorDTO struct {
	Name       string  `json:"name"`
	ExternalID *string `json:"externalId,omitempty"`
}

type ratingDTO struct {
	Average *float64 `json:"average"`
	Count   int      `json:"count"`
}

type fileDTO struct {
	ContentType  string `json:"contentType"`
	SizeBytes    int64  `json:"sizeBytes"`
	DownloadPath string `json:"downloadPath"`
}

type documentDTO struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Category        string     `json:"category"`
	Tags            []string   `json:"tags"`
	Status          string     `json:"status"`
	RejectionReason *string    `json:"rejectionReason"`
	Source          string     `json:"source"`
	Author          authorDTO  `json:"author"`
	Rating          ratingDTO  `json:"rating"`
	File            fileDTO    `json:"file"`
	CreatedAt       time.Time  `json:"createdAt"`
	PublishedAt     *time.Time `json:"publishedAt"`
}

type pageDTO struct {
	Items []documentDTO `json:"items"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
	Total int           `json:"total"`
}

// toRatingDTO devuelve el promedio con un decimal, o null si no hay calificaciones.
func toRatingDTO(r model.Rating) ratingDTO {
	if r.Count == 0 {
		return ratingDTO{Count: 0}
	}
	avg := math.Round(r.Average*10) / 10
	return ratingDTO{Average: &avg, Count: r.Count}
}

// toDocumentDTO arma la respuesta. downloadPath es la ruta de descarga de la
// API que responde; showExternalID dice si se puede mostrar author.externalId
// (sólo a la API key que registró el documento).
func toDocumentDTO(s model.Summary, downloadPath string, showExternalID bool) documentDTO {
	d := documentDTO{
		ID: s.ID, Title: s.Title, Description: s.Description, Category: s.Category,
		Tags: s.Tags, Status: string(s.Status), Source: string(s.Source),
		Author:    authorDTO{Name: s.Author.Name},
		Rating:    toRatingDTO(s.Rating),
		File:      fileDTO{ContentType: s.File.ContentType, SizeBytes: s.File.SizeBytes, DownloadPath: downloadPath},
		CreatedAt: s.CreatedAt.UTC(),
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if s.Status == model.StatusRejected {
		reason := s.RejectionReason
		d.RejectionReason = &reason
	}
	if s.Status == model.StatusApproved && s.PublishedAt != nil {
		t := s.PublishedAt.UTC()
		d.PublishedAt = &t
	}
	if showExternalID && s.Author.ExternalID != "" {
		ext := s.Author.ExternalID
		d.Author.ExternalID = &ext
	}
	return d
}

// ---------- Formulario multipart ----------

// maxBodySize es el máximo del cuerpo de una subida: el PDF (RN-20) más los
// campos del formulario.
const maxBodySize = service.MaxFileSize + 1<<20

// formFields son los campos de texto que se leen del formulario.
var formFields = map[string]int{
	"title": 600, "description": 8000, "category": 500, "tags": 1200,
	"authorName": 400, "authorExternalId": 400,
}

type upload struct {
	Fields  map[string]string
	File    service.File
	HasFile bool
}

type malformedError struct{ msg string }

func (e malformedError) Error() string { return e.msg }

// readUpload lee un multipart/form-data con el archivo en la parte "file".
// No carga en memoria más de MaxFileSize + 1 bytes del archivo: si es más
// grande, devuelve model.ErrFileTooLarge (413).
func readUpload(w http.ResponseWriter, r *http.Request) (upload, error) {
	up := upload{Fields: map[string]string{}}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		return up, malformedError{"el cuerpo tiene que ser multipart/form-data"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	mr, err := r.MultipartReader()
	if err != nil {
		return up, malformedError{"no se pudo leer el formulario: " + err.Error()}
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return up, bodyErr(err)
		}
		name := part.FormName()
		switch {
		case name == "file":
			data, err := io.ReadAll(io.LimitReader(part, service.MaxFileSize+1))
			if err != nil {
				return up, bodyErr(err)
			}
			if len(data) > service.MaxFileSize {
				return up, model.ErrFileTooLarge
			}
			up.File = service.File{Name: part.FileName(), ContentType: part.Header.Get("Content-Type"), Data: data}
			up.HasFile = true
		case formFields[name] > 0:
			b, err := io.ReadAll(io.LimitReader(part, int64(formFields[name])))
			if err != nil {
				return up, bodyErr(err)
			}
			up.Fields[name] = string(b)
		}
		_ = part.Close()
	}
	if !up.HasFile {
		return up, malformedError{`falta la parte "file" del formulario`}
	}
	return up, nil
}

func bodyErr(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return model.ErrFileTooLarge
	}
	return malformedError{"no se pudo leer el formulario: " + err.Error()}
}

// ---------- Parámetros ----------

// pagination lee page y size (RN-27: como máximo 50).
func pagination(r *http.Request) (page, size int, fields []model.FieldError) {
	page, size = 1, model.DefaultPageSize
	if v := r.URL.Query().Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fields = append(fields, model.FieldError{Field: "page", Message: "tiene que ser un entero desde 1"})
		} else {
			page = n
		}
	}
	if v := r.URL.Query().Get("size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > model.MaxPageSize {
			fields = append(fields, model.FieldError{Field: "size", Message: "tiene que ser un entero de 1 a 50"})
		} else {
			size = n
		}
	}
	return page, size, fields
}

// searchQuery lee los parámetros de la búsqueda (RF-19, RF-20). La categoría
// se lee de "category" y, en la API del foro, también de "subject".
func searchQuery(r *http.Request) (model.SearchQuery, error) {
	q := r.URL.Query()
	page, size, fields := pagination(r)
	sq := model.SearchQuery{Text: strings.TrimSpace(q.Get("q")), Page: page, Size: size}

	if utf8.RuneCountInString(sq.Text) > 200 {
		fields = append(fields, model.FieldError{Field: "q", Message: "tiene como máximo 200 caracteres"})
	}
	category := q.Get("category")
	if category == "" {
		category = q.Get("subject")
	}
	if utf8.RuneCountInString(category) > 120 {
		fields = append(fields, model.FieldError{Field: "category", Message: "tiene como máximo 120 caracteres"})
	}
	sq.Category = model.NormalizeKey(category)

	tags := q["tag"]
	if len(tags) > 5 {
		fields = append(fields, model.FieldError{Field: "tag", Message: "se permiten hasta 5 etiquetas"})
	}
	for _, t := range tags {
		if utf8.RuneCountInString(t) > 40 {
			fields = append(fields, model.FieldError{Field: "tag", Message: "cada etiqueta tiene como máximo 40 caracteres"})
			break
		}
		if k := model.NormalizeKey(t); k != "" {
			sq.Tags = append(sq.Tags, k)
		}
	}

	if v := q.Get("minRating"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 1 || f > 5 {
			fields = append(fields, model.FieldError{Field: "minRating", Message: "tiene que ser un número de 1 a 5"})
		} else {
			sq.MinRating = f
		}
	}
	switch s := model.Sort(q.Get("sort")); s {
	case "", model.SortRelevance, model.SortRating, model.SortDate:
		sq.Sort = s
	default:
		fields = append(fields, model.FieldError{Field: "sort", Message: "tiene que ser relevance, rating o date"})
	}
	if len(fields) > 0 {
		return sq, &model.ValidationError{Fields: fields}
	}
	return sq, nil
}

// ---------- Respuestas ----------

// fail traduce los errores del service a respuestas Problem Details con un
// code estable (ADR-005, contrato documents-v1).
func fail(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var verr *model.ValidationError
	var malformed malformedError
	switch {
	case errors.As(err, &verr):
		problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "hay campos inválidos", verr.Fields)
	case errors.As(err, &malformed):
		problem(w, r, http.StatusBadRequest, "MALFORMED_REQUEST", malformed.msg, nil)
	case errors.Is(err, model.ErrNotFound):
		problem(w, r, http.StatusNotFound, "NOT_FOUND", "el documento no existe", nil)
	case errors.Is(err, model.ErrForbidden):
		problem(w, r, http.StatusForbidden, "FORBIDDEN", "la operación es sólo para administradores", nil)
	case errors.Is(err, model.ErrAlreadyModerated):
		problem(w, r, http.StatusConflict, "ALREADY_MODERATED", "la publicación ya fue moderada", nil)
	case errors.Is(err, model.ErrNotApproved):
		problem(w, r, http.StatusUnprocessableEntity, "NOT_APPROVED", "sólo se pueden calificar publicaciones aprobadas", nil)
	case errors.Is(err, model.ErrOwnSummary):
		problem(w, r, http.StatusUnprocessableEntity, "OWN_SUMMARY", "no podés calificar tu propio resumen", nil)
	case errors.Is(err, model.ErrFileTooLarge):
		problem(w, r, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "el archivo pesa más de 10 MB", nil)
	case errors.Is(err, model.ErrUnsupportedFileType):
		problem(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_FILE_TYPE", "el archivo tiene que ser PDF", nil)
	case errors.Is(err, model.ErrIdempotencyReused):
		problem(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "la clave de idempotencia ya se usó con otro documento", nil)
	case errors.Is(err, model.ErrRequestInProgress):
		w.Header().Set("Retry-After", "2")
		problem(w, r, http.StatusConflict, "REQUEST_IN_PROGRESS", "hay un registro en curso con la misma clave de idempotencia", nil)
	case errors.Is(err, model.ErrSearchUnavailable):
		logger.ErrorContext(r.Context(), "búsqueda no disponible", "error", err.Error())
		w.Header().Set("Retry-After", "5")
		problem(w, r, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "la búsqueda no está disponible en este momento", nil)
	case errors.Is(err, model.ErrUnavailable):
		logger.ErrorContext(r.Context(), "dependencia no disponible", "error", err.Error())
		w.Header().Set("Retry-After", "5")
		problem(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "el servicio no está disponible en este momento, probá de nuevo en unos segundos", nil)
	default:
		logger.ErrorContext(r.Context(), "error interno", "error", err.Error())
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "error interno", nil)
	}
}

// problem responde en formato Problem Details (RFC 9457).
func problem(w http.ResponseWriter, r *http.Request, status int, code, detail string, fields []model.FieldError) {
	body := map[string]any{
		"type":          "about:blank",
		"title":         http.StatusText(status),
		"status":        status,
		"code":          code,
		"detail":        detail,
		"correlationId": correlation.ID(r.Context()),
	}
	if fields != nil {
		body["errors"] = fields
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// sendFile responde el PDF, sin cargarlo entero en memoria.
func sendFile(w http.ResponseWriter, doc model.Summary, body io.ReadCloser) {
	defer body.Close()
	w.Header().Set("Content-Type", service.PDFContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(doc.File.SizeBytes, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": doc.File.FileName}))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}
