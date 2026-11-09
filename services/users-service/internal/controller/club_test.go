package controller

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
)

// clubRepo agrega a memRepo una vinculación en memoria; sin puntos pendientes.
type clubRepo struct {
	*memRepo
	link *model.ClubLink
}

func (r *clubRepo) CreateClubLink(_ context.Context, l model.ClubLink) (model.ClubLink, error) {
	if r.link == nil {
		r.link = &l
	}
	return *r.link, nil
}

func (r *clubRepo) GetClubLink(context.Context, string) (model.ClubLink, error) {
	if r.link == nil {
		return model.ClubLink{}, model.ErrNotFound
	}
	return *r.link, nil
}

func (r *clubRepo) ActivateClubLink(context.Context, string, time.Time) (bool, error) {
	return false, nil
}
func (r *clubRepo) PendingClubLinks(context.Context, int) ([]model.ClubLink, error) { return nil, nil }
func (r *clubRepo) AddClubCredit(context.Context, model.ClubCredit) (bool, error)   { return true, nil }
func (r *clubRepo) PendingClubCredits(context.Context, string) ([]model.ClubCredit, error) {
	return nil, nil
}
func (r *clubRepo) PendingCreditsOfLinkedUsers(context.Context, int) ([]model.ClubCredit, error) {
	return nil, nil
}
func (r *clubRepo) SetClubCreditResult(context.Context, string, string, string, string, time.Time) error {
	return nil
}

// newClubHandler arma el handler con un Club que siempre responde 503.
func newClubHandler(t *testing.T) (http.Handler, *clubRepo) {
	t.Helper()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	repo := &clubRepo{memRepo: &memRepo{users: []model.User{{ID: "u1", Name: "Ana", Email: "ana@mail.com", Role: model.RoleStudent}}}}
	svc := &service.Service{Repo: repo.memRepo, Tokens: fakeTokens{}, BcryptCost: bcrypt.MinCost}
	clubSvc := &service.Club{Repo: repo, Provider: club.New(down.URL, "clave", logger), Logger: logger}
	h := &Handler{Svc: svc, Club: clubSvc, DB: repo.memRepo, Logger: logger, InternalToken: internalToken}
	return h.Routes(), repo
}

var fromGateway = map[string]string{InternalTokenHeader: internalToken, UserIDHeader: "u1"}

func TestClubSinPasarPorElGatewayDevuelve401(t *testing.T) {
	h, _ := newClubHandler(t)
	rec, body := do(t, h, http.MethodGet, "/me/club", "", map[string]string{UserIDHeader: "u1"})
	assertProblem(t, rec, body, http.StatusUnauthorized, "UNAUTHENTICATED")
}

func TestClubSinVincular(t *testing.T) {
	h, _ := newClubHandler(t)
	rec, body := do(t, h, http.MethodGet, "/me/club", "", fromGateway)
	if rec.Code != http.StatusOK || body["status"] != "sin_vincular" || body["balance"] != nil || body["warning"] != nil {
		t.Fatalf("status = %d, cuerpo = %v", rec.Code, body)
	}
}

// Con el Club caído, vincular queda pendiente con el aviso y el perfil se ve igual.
func TestClubCaidoAlVincularYAlConsultar(t *testing.T) {
	h, repo := newClubHandler(t)
	rec, body := do(t, h, http.MethodPost, "/me/club", "", fromGateway)
	if rec.Code != http.StatusAccepted || body["status"] != "pendiente" || body["warning"] != service.WarningClubUnavailable {
		t.Fatalf("POST: status = %d, cuerpo = %v", rec.Code, body)
	}
	repo.link.Status = model.ClubLinkActive
	rec, body = do(t, h, http.MethodGet, "/me/club", "", fromGateway)
	if rec.Code != http.StatusOK || body["status"] != "vinculada" || body["balance"] != nil || body["warning"] != service.WarningClubUnavailable {
		t.Fatalf("GET: status = %d, cuerpo = %v", rec.Code, body)
	}
}

func TestCambiarContraseñaPorHTTP(t *testing.T) {
	h, repo := newClubHandler(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("vieja-123"), bcrypt.MinCost)
	repo.users[0].PasswordHash = string(hash)
	rec, body := do(t, h, http.MethodPost, "/me/password", `{"currentPassword":"mal","newPassword":"nueva-123"}`, fromGateway)
	assertProblem(t, rec, body, http.StatusUnprocessableEntity, "INVALID_CURRENT_PASSWORD")
	rec, _ = do(t, h, http.MethodPost, "/me/password", `{"currentPassword":"vieja-123","newPassword":"nueva-123"}`, fromGateway)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
}
