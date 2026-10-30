package controller

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/apikey"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/ratelimit"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
)

// V1 es la capacidad publicada para otros grupos, tal como la describe
// docs/contracts/documents-v1.yaml (ADR-014). Se publica directo, sin pasar por
// el api-gateway: la autenticación es por API key en X-API-Key.
type V1 struct {
	Svc     *service.Service
	Keys    *apikey.Keys
	Limiter *ratelimit.Limiter
	Logger  *slog.Logger
}

// documentIDPattern es el formato del id en el contrato.
var documentIDPattern = regexp.MustCompile(`^[a-z0-9]{8,40}$`)

func (h *V1) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/documents", h.authorized(true, h.register))
	mux.HandleFunc("GET /v1/documents", h.authorized(false, h.search))
	mux.HandleFunc("GET /v1/documents/{documentId}", h.authorized(false, h.detail))
	mux.HandleFunc("GET /v1/documents/{documentId}/file", h.authorized(false, h.download))
	mux.HandleFunc("GET /v1/submissions", h.authorized(false, h.submissions))
}

type consumerHandler func(w http.ResponseWriter, r *http.Request, consumer string)

// authorized valida la API key y aplica el cupo de uso. Las respuestas
// informan el cupo en RateLimit-Limit y RateLimit-Remaining.
func (h *V1) authorized(register bool, next consumerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		consumer, ok := h.Keys.Lookup(r.Header.Get("X-API-Key"))
		if !ok {
			problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "falta el encabezado X-API-Key o no es válido", nil)
			return
		}
		d := h.Limiter.Allow(consumer, register)
		w.Header().Set("RateLimit-Limit", strconv.Itoa(d.Limit))
		w.Header().Set("RateLimit-Remaining", strconv.Itoa(d.Remaining))
		if !d.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(d.RetryAfter.Seconds()))))
			detail := "se superó el cupo de 60 pedidos por minuto"
			if register {
				detail = "se superó el cupo de 60 pedidos o de 10 registros por minuto"
			}
			problem(w, r, http.StatusTooManyRequests, "RATE_LIMITED", detail, nil)
			return
		}
		next(w, r, consumer)
	}
}

func v1Doc(s model.Summary, consumer string) documentDTO {
	return toDocumentDTO(s, "/v1/documents/"+s.ID+"/file", s.Author.Consumer != "" && s.Author.Consumer == consumer)
}

func v1Page(p model.Page, consumer string) pageDTO {
	out := pageDTO{Items: make([]documentDTO, 0, len(p.Items)), Page: p.Page, Size: p.Size, Total: p.Total}
	for _, s := range p.Items {
		out.Items = append(out.Items, v1Doc(s, consumer))
	}
	return out
}

// register: POST /v1/documents (RF-25). Responde 202 con el documento
// pendiente y Location; un reintento con la misma clave responde lo mismo con
// Idempotent-Replayed: true (RN-40).
func (h *V1) register(w http.ResponseWriter, r *http.Request, consumer string) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		problem(w, r, http.StatusBadRequest, "MALFORMED_REQUEST", "falta el encabezado Idempotency-Key", nil)
		return
	}
	if n := utf8.RuneCountInString(key); n < 8 || n > 100 {
		fail(w, r, h.Logger, &model.ValidationError{Fields: []model.FieldError{{Field: "Idempotency-Key", Message: "tiene que tener entre 8 y 100 caracteres"}}})
		return
	}
	up, err := readUpload(w, r)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	doc, replayed, err := h.Svc.Register(r.Context(), consumer, key, service.NewDocument{
		Title: up.Fields["title"], Description: up.Fields["description"],
		Category: up.Fields["category"], Tags: up.Fields["tags"],
		Author: model.Author{Name: up.Fields["authorName"], ExternalID: strings.TrimSpace(up.Fields["authorExternalId"])},
	}, up.File)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	w.Header().Set("Location", "/v1/documents/"+doc.ID)
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, http.StatusAccepted, v1Doc(doc, consumer))
}

// search: GET /v1/documents, sólo aprobados (RF-26, RN-39).
func (h *V1) search(w http.ResponseWriter, r *http.Request, consumer string) {
	q, err := searchQuery(r)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	page, err := h.Svc.SearchApproved(r.Context(), q)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, v1Page(page, consumer))
}

// get aplica RN-39: aprobado para cualquiera; pendiente o rechazado sólo para
// la API key que lo registró; eliminado o inexistente, 404.
func (h *V1) get(ctx context.Context, r *http.Request, consumer string) (model.Summary, error) {
	id := r.PathValue("documentId")
	if !documentIDPattern.MatchString(id) {
		return model.Summary{}, model.ErrNotFound
	}
	return h.Svc.Get(ctx, service.Viewer{Consumer: consumer}, id)
}

func (h *V1) detail(w http.ResponseWriter, r *http.Request, consumer string) {
	doc, err := h.get(r.Context(), r, consumer)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, v1Doc(doc, consumer))
}

func (h *V1) download(w http.ResponseWriter, r *http.Request, consumer string) {
	if !documentIDPattern.MatchString(r.PathValue("documentId")) {
		fail(w, r, h.Logger, model.ErrNotFound)
		return
	}
	doc, body, err := h.Svc.Download(r.Context(), service.Viewer{Consumer: consumer}, r.PathValue("documentId"))
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	sendFile(w, doc, body)
}

// submissions: GET /v1/submissions, lo registrado con esta API key (RF-27).
func (h *V1) submissions(w http.ResponseWriter, r *http.Request, consumer string) {
	page, size, fields := pagination(r)
	status := model.Status(r.URL.Query().Get("status"))
	switch status {
	case "", model.StatusPending, model.StatusApproved, model.StatusRejected:
	default:
		fields = append(fields, model.FieldError{Field: "status", Message: "tiene que ser pendiente, aprobado o rechazado"})
	}
	if fields != nil {
		fail(w, r, h.Logger, &model.ValidationError{Fields: fields})
		return
	}
	p, err := h.Svc.Submissions(r.Context(), consumer, status, page, size)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, v1Page(p, consumer))
}
