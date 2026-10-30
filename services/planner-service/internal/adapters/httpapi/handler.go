// Package httpapi es el adaptador de entrada HTTP de planner-service.
//
// La identidad del estudiante llega en el encabezado X-User-Id, que completa
// el api-gateway después de validar el JWT (ADR-001). Como el puerto de
// planner-service también queda publicado (ADR-008), X-User-Id sólo se acepta
// si el pedido trae X-Internal-Token con el secreto que comparte con el gateway.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/mcpserver"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	Svc    *app.Service
	DB     Pinger
	Logger *slog.Logger
	// InternalToken es el secreto que el api-gateway manda en X-Internal-Token.
	// Vacío, ningún pedido de estudiante se acepta.
	InternalToken string
}

// InternalTokenHeader es el encabezado con el secreto compartido con el gateway.
const InternalTokenHeader = "X-Internal-Token"

// Routes devuelve el router. Acepta las rutas con y sin el prefijo
// /api/planner, para funcionar igual detrás del api-gateway o llamado directo.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /subjects", h.withStudent(h.createSubject))
	mux.HandleFunc("GET /subjects", h.withStudent(h.listSubjects))
	mux.HandleFunc("DELETE /subjects/{subjectId}", h.withStudent(h.deleteSubject))
	mux.HandleFunc("GET /subjects/{subjectId}/materials", h.withStudent(h.listMaterials))
	mux.HandleFunc("POST /subjects/{subjectId}/materials", h.withStudent(h.addMaterial))
	mux.HandleFunc("DELETE /materials/{materialId}", h.withStudent(h.deleteMaterial))
	mux.HandleFunc("POST /exams", h.withStudent(h.createExam))
	mux.HandleFunc("GET /exams", h.withStudent(h.listExams))
	mux.HandleFunc("PATCH /exams/{examId}", h.withStudent(h.updateExam))
	mux.HandleFunc("POST /exams/{examId}/cancel", h.withStudent(h.cancelExam))
	mux.HandleFunc("GET /availability", h.withStudent(h.getAvailability))
	mux.HandleFunc("PUT /availability", h.withStudent(h.setAvailability))
	mux.HandleFunc("POST /exams/{examId}/proposals", h.withStudent(h.propose))
	mux.HandleFunc("POST /plans/{planId}/confirm", h.withStudent(h.confirm))
	mux.HandleFunc("GET /sessions", h.withStudent(h.listSessions))
	// Agente por MCP (SPEC 10.1): mismas reglas y misma identidad que la API REST.
	mux.HandleFunc("/mcp", h.withStudent(func(w http.ResponseWriter, r *http.Request, student string) {
		mcpserver.Handler(h.Svc, student).ServeHTTP(w, r)
	}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := strings.TrimPrefix(r.URL.Path, "/api/planner"); p != r.URL.Path {
			r.URL.Path = p
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}
		mux.ServeHTTP(w, r)
	})
}

type studentHandler func(w http.ResponseWriter, r *http.Request, studentID string)

func (h *Handler) withStudent(next studentHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.fromGateway(r) {
			h.problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "el pedido no pasó por el api-gateway", nil)
			return
		}
		id := strings.TrimSpace(r.Header.Get("X-User-Id"))
		if id == "" {
			h.problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "falta la identidad del estudiante (X-User-Id)", nil)
			return
		}
		next(w, r, id)
	}
}

// fromGateway compara el secreto interno en tiempo constante, para no dar
// pistas sobre su valor con el tiempo de respuesta.
func (h *Handler) fromGateway(r *http.Request) bool {
	got := r.Header.Get(InternalTokenHeader)
	return h.InternalToken != "" && got != "" &&
		subtle.ConstantTimeCompare([]byte(got), []byte(h.InternalToken)) == 1
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	if err := h.DB.Ping(ctx); err != nil {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status, "service": "planner-service"})
}

// ---------- DTOs ----------

type subjectDTO struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Professors []string `json:"professors"`
	Difficulty int      `json:"difficulty"`
}

type examDTO struct {
	ID        string   `json:"id"`
	SubjectID string   `json:"subjectId"`
	Type      string   `json:"type"`
	StartsAt  string   `json:"startsAt"` // RFC 3339, con zona horaria
	Topics    []string `json:"topics"`
	Status    string   `json:"status"`
	// PlanStatus es "confirmado" o "desactualizado" (RN-16); se omite si el
	// examen no tiene un plan confirmado.
	PlanStatus string `json:"planStatus,omitempty"`
	// Modality: "teorico", "practico", "mixto" o "" si no se indicó.
	Modality string `json:"modality"`
	// MaterialIDs: el material de la materia que entra; vacío = todo.
	MaterialIDs []string `json:"materialIds"`
}

func toExamDTO(e domain.Exam) examDTO {
	topics := e.Topics
	if topics == nil {
		topics = []string{}
	}
	materialIDs := e.MaterialIDs
	if materialIDs == nil {
		materialIDs = []string{}
	}
	return examDTO{e.ID, e.SubjectID, string(e.Type), e.StartsAt.Format(time.RFC3339), topics, string(e.Status), string(e.PlanStatus),
		string(e.Modality), materialIDs}
}

type materialDTO struct {
	ID        string    `json:"id"`
	SubjectID string    `json:"subjectId"`
	Kind      string    `json:"kind"` // "teorico" o "practico"
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

func toMaterialDTO(m domain.Material) materialDTO {
	return materialDTO{m.ID, m.SubjectID, string(m.Kind), m.Title, m.Content, m.CreatedAt.UTC()}
}

// examChangesDTO es el cuerpo de PATCH /exams/{examId}: sólo cambian los
// campos presentes.
type examChangesDTO struct {
	Type        *string   `json:"type"`
	StartsAt    *string   `json:"startsAt"`
	Topics      *[]string `json:"topics"`
	Modality    *string   `json:"modality"`
	MaterialIDs *[]string `json:"materialIds"`
}

type slotDTO struct {
	Weekday   int    `json:"weekday"` // 0 = domingo
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

type availabilityDTO struct {
	Slots                   []slotDTO `json:"weeklySlots"`
	MaxDailyMinutes         int       `json:"maxDailyMinutes"`
	PreferredSessionMinutes int       `json:"preferredSessionMinutes"`
	Timezone                string    `json:"timezone"`
}

type draftDTO struct {
	Start           time.Time `json:"start"`
	End             time.Time `json:"end"`
	DurationMinutes int       `json:"durationMinutes"`
	Topic           string    `json:"topic"`
	Objective       string    `json:"objective"`
	Reasoning       string    `json:"reasoning"`
}

type planDTO struct {
	ID          string     `json:"id"`
	ExamID      string     `json:"examId"`
	Status      string     `json:"status"`
	GeneratedBy string     `json:"generatedBy"`
	Warnings    []string   `json:"warnings"`
	Sessions    []draftDTO `json:"sessions"`
}

type sessionDTO struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"planId"`
	ExamID    string    `json:"examId"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Topic     string    `json:"topic"`
	Objective string    `json:"objective"`
	Status    string    `json:"status"`
	// Outdated: el examen cambió de fecha después de confirmar el plan (RN-16).
	Outdated bool `json:"outdated"`
}

func toSessionDTOs(in []domain.Session) []sessionDTO {
	out := make([]sessionDTO, len(in))
	for i, s := range in {
		out[i] = sessionDTO{s.ID, s.PlanID, s.ExamID, s.Start, s.End, s.Topic, s.Objective, string(s.Status), s.Outdated}
	}
	return out
}

// ---------- handlers ----------

func (h *Handler) createSubject(w http.ResponseWriter, r *http.Request, student string) {
	var in subjectDTO
	if !h.decode(w, r, &in) {
		return
	}
	s, err := h.Svc.CreateSubject(r.Context(), domain.Subject{StudentID: student, Name: in.Name, Professors: in.Professors, Difficulty: domain.Difficulty(in.Difficulty)})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, subjectDTO{s.ID, s.Name, s.Professors, int(s.Difficulty)})
}

func (h *Handler) listSubjects(w http.ResponseWriter, r *http.Request, student string) {
	list, err := h.Svc.ListSubjects(r.Context(), student)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]subjectDTO, len(list))
	for i, s := range list {
		out[i] = subjectDTO{s.ID, s.Name, s.Professors, int(s.Difficulty)}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createExam(w http.ResponseWriter, r *http.Request, student string) {
	var in examDTO
	if !h.decode(w, r, &in) {
		return
	}
	startsAt, err := time.Parse(time.RFC3339, in.StartsAt)
	if err != nil {
		h.problem(w, r, http.StatusBadRequest, "INVALID_DATE", "startsAt tiene que tener formato RFC 3339 (por ejemplo 2026-10-28T09:00:00-03:00)", nil)
		return
	}
	e, err := h.Svc.CreateExam(r.Context(), domain.Exam{StudentID: student, SubjectID: in.SubjectID, Type: domain.ExamType(in.Type), StartsAt: startsAt, Topics: in.Topics,
		Modality: domain.ExamModality(in.Modality), MaterialIDs: in.MaterialIDs})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toExamDTO(e))
}

func (h *Handler) listExams(w http.ResponseWriter, r *http.Request, student string) {
	list, err := h.Svc.ListExams(r.Context(), student)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]examDTO, len(list))
	for i, e := range list {
		out[i] = toExamDTO(e)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) updateExam(w http.ResponseWriter, r *http.Request, student string) {
	var in examChangesDTO
	if !h.decode(w, r, &in) {
		return
	}
	var ch app.ExamChanges
	if in.Type != nil {
		t := domain.ExamType(*in.Type)
		ch.Type = &t
	}
	if in.StartsAt != nil {
		startsAt, err := time.Parse(time.RFC3339, *in.StartsAt)
		if err != nil {
			h.problem(w, r, http.StatusBadRequest, "INVALID_DATE", "startsAt tiene que tener formato RFC 3339 (por ejemplo 2026-10-28T09:00:00-03:00)", nil)
			return
		}
		ch.StartsAt = &startsAt
	}
	ch.Topics = in.Topics
	if in.Modality != nil {
		m := domain.ExamModality(*in.Modality)
		ch.Modality = &m
	}
	ch.MaterialIDs = in.MaterialIDs
	e, err := h.Svc.UpdateExam(r.Context(), student, r.PathValue("examId"), ch)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.InfoContext(r.Context(), "examen editado", "examId", e.ID, "planStatus", e.PlanStatus)
	writeJSON(w, http.StatusOK, toExamDTO(e))
}

func (h *Handler) cancelExam(w http.ResponseWriter, r *http.Request, student string) {
	e, err := h.Svc.CancelExam(r.Context(), student, r.PathValue("examId"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.InfoContext(r.Context(), "examen cancelado", "examId", e.ID)
	writeJSON(w, http.StatusOK, toExamDTO(e))
}

func (h *Handler) listMaterials(w http.ResponseWriter, r *http.Request, student string) {
	list, err := h.Svc.ListMaterials(r.Context(), student, r.PathValue("subjectId"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]materialDTO, len(list))
	for i, m := range list {
		out[i] = toMaterialDTO(m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) addMaterial(w http.ResponseWriter, r *http.Request, student string) {
	var in struct {
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	m, err := h.Svc.AddMaterial(r.Context(), domain.Material{StudentID: student, SubjectID: r.PathValue("subjectId"),
		Kind: domain.MaterialKind(in.Kind), Title: in.Title, Content: in.Content})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMaterialDTO(m))
}

func (h *Handler) deleteMaterial(w http.ResponseWriter, r *http.Request, student string) {
	if err := h.Svc.DeleteMaterial(r.Context(), student, r.PathValue("materialId")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseClock(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func (h *Handler) setAvailability(w http.ResponseWriter, r *http.Request, student string) {
	var in availabilityDTO
	if !h.decode(w, r, &in) {
		return
	}
	loc, err := time.LoadLocation(in.Timezone)
	if err != nil || in.Timezone == "" {
		h.problem(w, r, http.StatusBadRequest, "INVALID_TIMEZONE", "timezone tiene que ser una zona IANA (por ejemplo America/Argentina/Cordoba)", nil)
		return
	}
	av := domain.Availability{StudentID: student, MaxDailyMinutes: in.MaxDailyMinutes, PreferredSessionMinutes: in.PreferredSessionMinutes, Location: loc}
	for _, s := range in.Slots {
		start, ok1 := parseClock(s.StartTime)
		end, ok2 := parseClock(s.EndTime)
		if !ok1 || !ok2 || s.Weekday < 0 || s.Weekday > 6 {
			h.problem(w, r, http.StatusBadRequest, "INVALID_SLOT", "cada franja necesita weekday de 0 a 6 y horas HH:mm", nil)
			return
		}
		av.Slots = append(av.Slots, domain.Slot{Weekday: time.Weekday(s.Weekday), StartMinute: start, EndMinute: end})
	}
	if err := h.Svc.SetAvailability(r.Context(), av); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteSubject borra una materia con sus exámenes: 204, o 404 si no existe
// o ya estaba borrada.
func (h *Handler) deleteSubject(w http.ResponseWriter, r *http.Request, student string) {
	if err := h.Svc.DeleteSubject(r.Context(), student, r.PathValue("subjectId")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getAvailability devuelve la disponibilidad con la misma forma que recibe el
// PUT, así el frontend la lee del servidor y no sólo del navegador.
func (h *Handler) getAvailability(w http.ResponseWriter, r *http.Request, student string) {
	av, err := h.Svc.Availability(r.Context(), student)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	slots := append([]domain.Slot(nil), av.Slots...)
	sort.Slice(slots, func(a, b int) bool {
		if slots[a].Weekday != slots[b].Weekday {
			return slots[a].Weekday < slots[b].Weekday
		}
		return slots[a].StartMinute < slots[b].StartMinute
	})
	out := availabilityDTO{Slots: []slotDTO{}, MaxDailyMinutes: av.MaxDailyMinutes, PreferredSessionMinutes: av.PreferredSessionMinutes, Timezone: "UTC"}
	if av.Location != nil {
		out.Timezone = av.Location.String()
	}
	for _, s := range slots {
		out.Slots = append(out.Slots, slotDTO{Weekday: int(s.Weekday), StartTime: formatClock(s.StartMinute), EndTime: formatClock(s.EndMinute)})
	}
	writeJSON(w, http.StatusOK, out)
}

func formatClock(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request, student string) {
	p, err := h.Svc.ProposePlan(r.Context(), student, r.PathValue("examId"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := planDTO{ID: p.ID, ExamID: p.ExamID, Status: string(p.Status), GeneratedBy: string(p.GeneratedBy), Warnings: p.Warnings}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	for _, d := range p.Drafts {
		out.Sessions = append(out.Sessions, draftDTO{d.Start, d.End(), d.DurationMinutes, d.Topic, d.Objective, d.Reasoning})
	}
	h.Logger.InfoContext(r.Context(), "propuesta generada", "planId", p.ID, "generatedBy", p.GeneratedBy, "sessions", len(p.Drafts))
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request, student string) {
	planID := r.PathValue("planId")
	res, err := h.Svc.ConfirmPlan(r.Context(), student, planID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.InfoContext(r.Context(), "plan confirmado", "planId", planID, "sessions", len(res.Sessions))
	writeJSON(w, http.StatusOK, map[string]any{"planId": res.PlanID, "status": "confirmado", "sessions": toSessionDTOs(res.Sessions)})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request, student string) {
	list, err := h.Svc.ListSessions(r.Context(), student)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSessionDTOs(list))
}

// ---------- errores (Problem Details, RFC 9457) ----------

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		h.problem(w, r, http.StatusBadRequest, "MALFORMED_REQUEST", "no se pudo leer el cuerpo: "+err.Error(), nil)
		return false
	}
	return true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		h.problem(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", "la operación incumple reglas de negocio", verr.Violations)
	case errors.Is(err, domain.ErrAvailabilityNotSet):
		h.problem(w, r, http.StatusNotFound, "AVAILABILITY_NOT_SET", "todavía no cargaste tu disponibilidad", nil)
	case errors.Is(err, domain.ErrNotFound):
		h.problem(w, r, http.StatusNotFound, "NOT_FOUND", "el recurso no existe", nil)
	case errors.Is(err, domain.ErrPlanNotProposed):
		h.problem(w, r, http.StatusConflict, "PLAN_NOT_PROPOSED", err.Error(), nil)
	case errors.Is(err, domain.ErrAlreadyConfirmed):
		h.problem(w, r, http.StatusConflict, "PLAN_ALREADY_CONFIRMED", err.Error(), nil)
	case errors.Is(err, domain.ErrConfirmInProgress):
		h.problem(w, r, http.StatusConflict, "REQUEST_IN_PROGRESS", err.Error(), nil)
	case errors.Is(err, domain.ErrExamCancelled):
		h.problem(w, r, http.StatusConflict, "EXAM_CANCELLED", err.Error(), nil)
	case errors.Is(err, domain.ErrIdempotencyReused):
		h.problem(w, r, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", err.Error(), nil)
	case errors.Is(err, domain.ErrUnavailable):
		h.Logger.ErrorContext(r.Context(), "dependencia no disponible", "error", err.Error())
		w.Header().Set("Retry-After", "5")
		h.problem(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "el servicio no está disponible en este momento, probá de nuevo en unos segundos", nil)
	case errors.Is(err, context.DeadlineExceeded):
		h.problem(w, r, http.StatusServiceUnavailable, "TIMEOUT", "la operación tardó demasiado", nil)
	default:
		h.Logger.ErrorContext(r.Context(), "error interno", "error", err.Error())
		h.problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "error interno", nil)
	}
}

func (h *Handler) problem(w http.ResponseWriter, r *http.Request, status int, code, detail string, violations []domain.RuleViolation) {
	body := map[string]any{
		"type":          "about:blank",
		"title":         http.StatusText(status),
		"status":        status,
		"code":          code,
		"detail":        detail,
		"correlationId": CorrelationID(r.Context()),
	}
	if violations != nil {
		body["errors"] = violations
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
