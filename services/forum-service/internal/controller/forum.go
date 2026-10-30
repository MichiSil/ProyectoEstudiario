package controller

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
)

// Encabezados que completa el api-gateway después de validar el JWT (ADR-001).
// X-User-Name y X-User-Email vienen codificados con url.QueryEscape y sólo si
// el token los trae.
const (
	UserIDHeader        = "X-User-Id"
	UserRoleHeader      = "X-User-Role"
	UserNameHeader      = "X-User-Name"
	UserEmailHeader     = "X-User-Email"
	InternalTokenHeader = "X-Internal-Token"
	roleAdmin           = "admin"
)

// defaultStudentName es el autor visible de un resumen cuando ni el token ni el
// formulario traen el nombre (por ejemplo, con un token de prueba de devtoken).
const defaultStudentName = "Estudiante de Estudiario"

// Forum es la API del foro para el frontend, detrás del api-gateway en
// /api/forum/*. Todas las rutas (salvo /health) exigen el secreto interno del
// gateway y la identidad que pone en X-User-Id y X-User-Role.
type Forum struct {
	Svc           *service.Service
	DB            Pinger
	Logger        *slog.Logger
	InternalToken string
}

func (h *Forum) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /summaries", h.withViewer(h.publish))
	mux.HandleFunc("GET /summaries", h.withViewer(h.search))
	mux.HandleFunc("GET /summaries/mine", h.withViewer(h.mine))
	mux.HandleFunc("GET /summaries/{id}", h.withViewer(h.detail))
	mux.HandleFunc("GET /summaries/{id}/file", h.withViewer(h.download))
	mux.HandleFunc("PUT /summaries/{id}/rating", h.withViewer(h.rate))

	mux.HandleFunc("GET /admin/summaries/pending", h.withAdmin(h.pending))
	mux.HandleFunc("POST /admin/summaries/{id}/approve", h.withAdmin(h.approve))
	mux.HandleFunc("POST /admin/summaries/{id}/reject", h.withAdmin(h.reject))
	mux.HandleFunc("DELETE /admin/summaries/{id}", h.withAdmin(h.delete))
}

type viewerHandler func(w http.ResponseWriter, r *http.Request, v service.Viewer)

func (h *Forum) withViewer(next viewerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get(InternalTokenHeader)
		if h.InternalToken == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.InternalToken)) != 1 {
			problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "el pedido no pasó por el api-gateway", nil)
			return
		}
		id := strings.TrimSpace(r.Header.Get(UserIDHeader))
		if id == "" {
			problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "falta la identidad del usuario (X-User-Id)", nil)
			return
		}
		next(w, r, service.Viewer{
			UserID: id,
			Admin:  r.Header.Get(UserRoleHeader) == roleAdmin,
			Name:   headerValue(r, UserNameHeader),
			Email:  headerValue(r, UserEmailHeader),
		})
	}
}

// headerValue decodifica un encabezado que el gateway mandó con url.QueryEscape.
// Si no se puede decodificar, se ignora.
func headerValue(r *http.Request, name string) string {
	v, err := url.QueryUnescape(r.Header.Get(name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// withAdmin deja pasar sólo a los administradores; un estudiante recibe 403.
func (h *Forum) withAdmin(next viewerHandler) http.HandlerFunc {
	return h.withViewer(func(w http.ResponseWriter, r *http.Request, v service.Viewer) {
		if !v.Admin {
			fail(w, r, h.Logger, model.ErrForbidden)
			return
		}
		next(w, r, v)
	})
}

func forumDoc(s model.Summary) documentDTO {
	return toDocumentDTO(s, "/api/forum/summaries/"+s.ID+"/file", false)
}

func forumPage(p model.Page) pageDTO {
	out := pageDTO{Items: make([]documentDTO, 0, len(p.Items)), Page: p.Page, Size: p.Size, Total: p.Total}
	for _, s := range p.Items {
		out.Items = append(out.Items, forumDoc(s))
	}
	return out
}

// publish sube un resumen (RF-14): multipart con title, category (la materia),
// description, tags y el PDF en file. El autor sale del usuario logueado: el
// nombre y el mail del token se copian en el resumen (ADR-003). Si el token no
// trae el nombre, se usa el campo authorName y, si tampoco viene, uno genérico.
func (h *Forum) publish(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	up, err := readUpload(w, r)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	name := v.Name
	if name == "" {
		name = strings.TrimSpace(up.Fields["authorName"])
	}
	if name == "" {
		name = defaultStudentName
	}
	doc, err := h.Svc.Publish(r.Context(), service.NewDocument{
		Title: up.Fields["title"], Description: up.Fields["description"],
		Category: up.Fields["category"], Tags: up.Fields["tags"],
		Author: model.Author{UserID: v.UserID, Name: name, Email: v.Email},
		Source: model.SourceEstudiario,
	}, up.File)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusCreated, forumDoc(doc))
}

// search lista los aprobados con búsqueda, filtros, orden y página (RF-19, RF-20).
func (h *Forum) search(w http.ResponseWriter, r *http.Request, _ service.Viewer) {
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
	writeJSON(w, http.StatusOK, forumPage(page))
}

// mine lista las publicaciones del estudiante con su estado (RF-15).
func (h *Forum) mine(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	page, size, fields := pagination(r)
	if fields != nil {
		fail(w, r, h.Logger, &model.ValidationError{Fields: fields})
		return
	}
	p, err := h.Svc.MySummaries(r.Context(), v, page, size)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, forumPage(p))
}

// detail devuelve un resumen aprobado, o uno propio en cualquier estado (RF-21).
func (h *Forum) detail(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	doc, err := h.Svc.Get(r.Context(), v, r.PathValue("id"))
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, forumDoc(doc))
}

func (h *Forum) download(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	doc, body, err := h.Svc.Download(r.Context(), v, r.PathValue("id"))
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	sendFile(w, doc, body)
}

// rate califica un resumen de 1 a 5 (RF-22). Cuerpo: {"score": 4}.
func (h *Forum) rate(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	var in struct {
		Score int `json:"score"`
	}
	if !decode(w, r, &in) {
		return
	}
	rating, err := h.Svc.Rate(r.Context(), v, r.PathValue("id"), in.Score)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toRatingDTO(rating))
}

// pending lista las publicaciones pendientes, de la más vieja a la más nueva (RF-16).
func (h *Forum) pending(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	page, size, fields := pagination(r)
	if fields != nil {
		fail(w, r, h.Logger, &model.ValidationError{Fields: fields})
		return
	}
	p, err := h.Svc.Pending(r.Context(), v, page, size)
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, forumPage(p))
}

func (h *Forum) approve(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	h.afterModeration(w, r, v, h.Svc.Approve(r.Context(), v, r.PathValue("id")))
}

// reject rechaza con motivo (RF-17, RN-22). Cuerpo: {"reason": "..."}.
func (h *Forum) reject(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	h.afterModeration(w, r, v, h.Svc.Reject(r.Context(), v, r.PathValue("id"), in.Reason))
}

// afterModeration responde el resumen ya moderado.
func (h *Forum) afterModeration(w http.ResponseWriter, r *http.Request, v service.Viewer, err error) {
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	doc, err := h.Svc.Get(r.Context(), v, r.PathValue("id"))
	if err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	writeJSON(w, http.StatusOK, forumDoc(doc))
}

func (h *Forum) delete(w http.ResponseWriter, r *http.Request, v service.Viewer) {
	if err := h.Svc.Delete(r.Context(), v, r.PathValue("id")); err != nil {
		fail(w, r, h.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		problem(w, r, http.StatusBadRequest, "MALFORMED_REQUEST", "no se pudo leer el cuerpo: "+err.Error(), nil)
		return false
	}
	return true
}
