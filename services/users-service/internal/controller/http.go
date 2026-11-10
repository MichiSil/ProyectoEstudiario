// Package controller es la capa HTTP de users-service: lee el pedido, llama al
// service y arma la respuesta. No tiene reglas de negocio (ADR-002).
//
// POST /register y POST /login son públicos. Las rutas /me usan la identidad que pone
// el api-gateway en X-User-Id después de validar el JWT, y sólo la acepta si el
// pedido trae X-Internal-Token con el secreto que comparte con el gateway: el
// puerto de users-service queda publicado y, sin esto, cualquiera podría leer
// los datos de otro usuario mandando su id.
package controller

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/observability"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
)

// Encabezados que completa el api-gateway.
const (
	UserIDHeader        = "X-User-Id"
	InternalTokenHeader = "X-Internal-Token"
)

// Pinger permite que /health verifique la base.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	Svc           *service.Service
	Club          *service.Club
	DB            Pinger
	Logger        *slog.Logger
	InternalToken string
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /register", h.register)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("GET /me", h.me)
	mux.HandleFunc("POST /me/password", h.changePassword)
	mux.HandleFunc("GET /me/club", h.clubStatus)
	mux.HandleFunc("POST /me/club", h.clubLink)
	return mux
}

// ---------- DTOs ----------

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userDTO es lo que se muestra de un usuario: nunca el hash de la contraseña.
type userDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      userDTO   `json:"user"`
}

func toDTO(u model.User) userDTO {
	return userDTO{ID: u.ID, Name: u.Name, Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt.UTC()}
}

// ---------- Handlers ----------

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	if err := h.DB.Ping(ctx); err != nil {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status, "service": "users-service"})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in registerRequest
	if !h.decode(w, r, &in) {
		return
	}
	u, err := h.Svc.Register(r.Context(), service.RegisterInput{Name: in.Name, Email: in.Email, Password: in.Password})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(u))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if !h.decode(w, r, &in) {
		return
	}
	var fields []model.FieldError
	if strings.TrimSpace(in.Email) == "" {
		fields = append(fields, model.FieldError{Field: "email", Message: "el mail es obligatorio"})
	}
	if in.Password == "" {
		fields = append(fields, model.FieldError{Field: "password", Message: "la contraseña es obligatoria"})
	}
	if len(fields) > 0 {
		h.fail(w, r, &model.ValidationError{Fields: fields})
		return
	}
	res, err := h.Svc.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: res.Token, ExpiresAt: res.ExpiresAt.UTC(), User: toDTO(res.User)})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	id, ok := h.identity(w, r)
	if !ok {
		return
	}
	u, err := h.Svc.Me(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(u))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := h.identity(w, r)
	if !ok {
		return
	}
	var in changePasswordRequest
	if !h.decode(w, r, &in) {
		return
	}
	if err := h.Svc.ChangePassword(r.Context(), id, in.CurrentPassword, in.NewPassword); err != nil {
		if errors.Is(err, model.ErrInvalidCredentials) {
			h.problem(w, r, http.StatusUnprocessableEntity, "INVALID_CURRENT_PASSWORD", "la contraseña actual no es correcta", nil)
			return
		}
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clubDTO es el estado del Club de Beneficios que ve el usuario en su perfil.
type clubDTO struct {
	Status        string        `json:"status"`
	Balance       *int          `json:"balance"`
	PendingPoints int           `json:"pendingPoints"`
	Movements     []movementDTO `json:"movements"`
	Warning       *string       `json:"warning"`
}

type movementDTO struct {
	ID         string    `json:"id"`
	Points     int       `json:"points"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurredAt"`
}

func toClubDTO(st service.ClubStatus) clubDTO {
	out := clubDTO{Status: st.Status, Balance: st.Balance, PendingPoints: st.PendingPoints, Movements: []movementDTO{}}
	for _, m := range st.Movements {
		out.Movements = append(out.Movements, movementDTO{ID: m.ID, Points: m.Points, Reason: m.Reason, OccurredAt: m.OccurredAt.UTC()})
	}
	if st.Warning != "" {
		out.Warning = &st.Warning
	}
	return out
}

// clubStatus devuelve el estado de la vinculación, el saldo y los últimos
// movimientos. Si el Club no responde, igual 200 con warning CLUB_UNAVAILABLE
// (ADR-009, 5): el perfil se muestra y sólo falta el saldo.
func (h *Handler) clubStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := h.identity(w, r)
	if !ok {
		return
	}
	st, err := h.Club.Status(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toClubDTO(st))
}

// clubLink pide vincular la cuenta del Club. Responde 202: la vinculación
// termina cuando la persona confirma el mail que le manda el Club.
func (h *Handler) clubLink(w http.ResponseWriter, r *http.Request) {
	id, ok := h.identity(w, r)
	if !ok {
		return
	}
	st, err := h.Club.Link(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	code := http.StatusAccepted
	if st.Status == model.ClubLinkActive {
		code = http.StatusOK
	}
	writeJSON(w, code, toClubDTO(st))
}

// identity devuelve el id del usuario que puso el api-gateway, o responde 401.
func (h *Handler) identity(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !h.fromGateway(r) {
		h.problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "el pedido no pasó por el api-gateway", nil)
		return "", false
	}
	id := strings.TrimSpace(r.Header.Get(UserIDHeader))
	if id == "" {
		h.problem(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "falta la identidad del usuario (X-User-Id)", nil)
		return "", false
	}
	return id, true
}

// fromGateway compara el secreto interno en tiempo constante.
func (h *Handler) fromGateway(r *http.Request) bool {
	got := r.Header.Get(InternalTokenHeader)
	return h.InternalToken != "" && got != "" &&
		subtle.ConstantTimeCompare([]byte(got), []byte(h.InternalToken)) == 1
}

// ---------- Respuestas ----------

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		h.problem(w, r, http.StatusBadRequest, "MALFORMED_REQUEST", "no se pudo leer el cuerpo: "+err.Error(), nil)
		return false
	}
	return true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var verr *model.ValidationError
	switch {
	case errors.As(err, &verr):
		h.problem(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "el pedido tiene datos inválidos", verr.Fields)
	case errors.Is(err, model.ErrEmailInUse):
		h.problem(w, r, http.StatusConflict, "EMAIL_IN_USE", "ya existe una cuenta con ese mail", nil)
	case errors.Is(err, model.ErrInvalidCredentials):
		h.problem(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "mail o contraseña incorrectos", nil)
	case errors.Is(err, model.ErrNotFound):
		h.problem(w, r, http.StatusNotFound, "NOT_FOUND", "el usuario no existe", nil)
	case errors.Is(err, club.ErrRejected):
		h.Logger.ErrorContext(r.Context(), "el Club rechazó el pedido", "error", err.Error())
		h.problem(w, r, http.StatusUnprocessableEntity, "CLUB_REJECTED", "el Club de Beneficios no aceptó tus datos; revisá que tu mail sea válido", nil)
	case errors.Is(err, club.ErrMisconfigured), errors.Is(err, club.ErrUnavailable):
		h.Logger.ErrorContext(r.Context(), "el Club no está disponible", "error", err.Error())
		w.Header().Set("Retry-After", "30")
		h.problem(w, r, http.StatusServiceUnavailable, "CLUB_UNAVAILABLE", "el Club de Beneficios no está disponible ahora, probá más tarde", nil)
	case errors.Is(err, model.ErrUnavailable):
		h.Logger.ErrorContext(r.Context(), "dependencia no disponible", "error", err.Error())
		w.Header().Set("Retry-After", "5")
		h.problem(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "el servicio no está disponible en este momento, probá de nuevo en unos segundos", nil)
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		// Cortó quien hizo el pedido (el gateway por su timeout, o el cliente):
		// nadie va a leer la respuesta y no es un error del servicio, así que
		// no se cuenta como 500 (POSTMORTEM del 2026-11-10, acción 3).
		h.Logger.WarnContext(r.Context(), "el pedido se canceló antes de terminar", "error", err.Error())
		w.WriteHeader(499) // convención de nginx: el cliente cerró el pedido
	default:
		h.Logger.ErrorContext(r.Context(), "error interno", "error", err.Error())
		h.problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "error interno", nil)
	}
}

// problem responde en formato Problem Details (RFC 9457), como el resto de los servicios (ADR-005).
func (h *Handler) problem(w http.ResponseWriter, r *http.Request, status int, code, detail string, fields []model.FieldError) {
	body := map[string]any{
		"type":          "about:blank",
		"title":         http.StatusText(status),
		"status":        status,
		"code":          code,
		"detail":        detail,
		"correlationId": observability.CorrelationID(r.Context()),
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
