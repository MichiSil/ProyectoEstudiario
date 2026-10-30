package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
)

const internalToken = "secreto-interno-de-prueba-con-32-caracteres"

type memRepo struct {
	mu    sync.Mutex
	users []model.User
	err   error // si no es nil, todas las operaciones fallan con este error
}

func (m *memRepo) Create(_ context.Context, u model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	for _, x := range m.users {
		if x.Email == u.Email {
			return model.ErrEmailInUse
		}
	}
	m.users = append(m.users, u)
	return nil
}

func (m *memRepo) find(match func(model.User) bool) (model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return model.User{}, m.err
	}
	for _, u := range m.users {
		if match(u) {
			return u, nil
		}
	}
	return model.User{}, model.ErrNotFound
}

func (m *memRepo) GetByEmail(_ context.Context, email string) (model.User, error) {
	return m.find(func(u model.User) bool { return u.Email == email })
}

func (m *memRepo) GetByID(_ context.Context, id string) (model.User, error) {
	return m.find(func(u model.User) bool { return u.ID == id })
}

func (m *memRepo) UpdateRole(_ context.Context, id, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	for i := range m.users {
		if m.users[i].ID == id {
			m.users[i].Role = role
			return nil
		}
	}
	return model.ErrNotFound
}

func (m *memRepo) Ping(context.Context) error { return m.err }

type fakeTokens struct{}

func (fakeTokens) Issue(u model.User) (string, time.Time, error) {
	return "jwt-" + u.ID, time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC), nil
}

func newHandler(repo *memRepo) http.Handler {
	svc := &service.Service{Repo: repo, Tokens: fakeTokens{}, AdminEmails: map[string]bool{}, BcryptCost: bcrypt.MinCost}
	h := &Handler{Svc: svc, DB: repo, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), InternalToken: internalToken}
	return h.Routes()
}

func do(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const registro = `{"name":"Ana","email":"ana@mail.com","password":"secreta123"}`

func TestRegisterDevuelve201SinElHash(t *testing.T) {
	rec, body := do(t, newHandler(&memRepo{}), http.MethodPost, "/register", registro, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, se esperaba 201: %s", rec.Code, rec.Body)
	}
	if body["email"] != "ana@mail.com" || body["role"] != "estudiante" || body["id"] == "" {
		t.Errorf("cuerpo = %v", body)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "password") || strings.Contains(rec.Body.String(), "$2a$") {
		t.Errorf("la respuesta expone la contraseña o su hash: %s", rec.Body)
	}
}

func TestRegisterConMailRepetidoDevuelve409(t *testing.T) {
	h := newHandler(&memRepo{})
	do(t, h, http.MethodPost, "/register", registro, nil)
	rec, body := do(t, h, http.MethodPost, "/register", registro, nil)
	assertProblem(t, rec, body, http.StatusConflict, "EMAIL_IN_USE")
}

func TestRegisterConDatosInvalidosDevuelve422(t *testing.T) {
	rec, body := do(t, newHandler(&memRepo{}), http.MethodPost, "/register",
		`{"name":"Ana","email":"ana@mail.com","password":"corta"}`, nil)
	assertProblem(t, rec, body, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 {
		t.Errorf("errors = %v, se esperaba el error de la contraseña", body["errors"])
	}
}

func TestLoginDevuelveTokenExpiresAtYUser(t *testing.T) {
	h := newHandler(&memRepo{})
	do(t, h, http.MethodPost, "/register", registro, nil)

	rec, body := do(t, h, http.MethodPost, "/login", `{"email":"ana@mail.com","password":"secreta123"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	user, _ := body["user"].(map[string]any)
	if body["token"] != "jwt-"+user["id"].(string) || body["expiresAt"] != "2026-10-29T08:00:00Z" || user["email"] != "ana@mail.com" {
		t.Errorf("cuerpo = %v", body)
	}
}

func TestLoginInvalidoDevuelve401ConElMismoMensaje(t *testing.T) {
	h := newHandler(&memRepo{})
	do(t, h, http.MethodPost, "/register", registro, nil)

	recClave, bodyClave := do(t, h, http.MethodPost, "/login", `{"email":"ana@mail.com","password":"otraclave1"}`, nil)
	recMail, bodyMail := do(t, h, http.MethodPost, "/login", `{"email":"nadie@mail.com","password":"secreta123"}`, nil)

	assertProblem(t, recClave, bodyClave, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	assertProblem(t, recMail, bodyMail, http.StatusUnauthorized, "INVALID_CREDENTIALS")
	if bodyClave["detail"] != bodyMail["detail"] {
		t.Errorf("los mensajes distinguen si el mail existe: %q / %q", bodyClave["detail"], bodyMail["detail"])
	}
}

func TestMeExigeElSecretoInternoYLaIdentidad(t *testing.T) {
	h := newHandler(&memRepo{})
	_, creado := do(t, h, http.MethodPost, "/register", registro, nil)
	id := creado["id"].(string)

	rec, body := do(t, h, http.MethodGet, "/me", "", map[string]string{"X-User-Id": id})
	assertProblem(t, rec, body, http.StatusUnauthorized, "UNAUTHENTICATED")

	rec, body = do(t, h, http.MethodGet, "/me", "", map[string]string{"X-User-Id": id, "X-Internal-Token": "otro-secreto"})
	assertProblem(t, rec, body, http.StatusUnauthorized, "UNAUTHENTICATED")

	rec, body = do(t, h, http.MethodGet, "/me", "", map[string]string{"X-Internal-Token": internalToken})
	assertProblem(t, rec, body, http.StatusUnauthorized, "UNAUTHENTICATED")

	rec, body = do(t, h, http.MethodGet, "/me", "", map[string]string{"X-User-Id": id, "X-Internal-Token": internalToken})
	if rec.Code != http.StatusOK || body["id"] != id {
		t.Fatalf("status = %d, cuerpo = %v", rec.Code, body)
	}
}

func TestBaseNoDisponibleDevuelve503(t *testing.T) {
	repo := &memRepo{err: errors.Join(model.ErrUnavailable, errors.New("timeout"))}
	rec, body := do(t, newHandler(repo), http.MethodPost, "/login", `{"email":"ana@mail.com","password":"secreta123"}`, nil)
	assertProblem(t, rec, body, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")

	rec, _ = do(t, newHandler(repo), http.MethodGet, "/health", "", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("health con la base caída = %d, se esperaba 503", rec.Code)
	}
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, body map[string]any, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, se esperaba %d: %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, se esperaba application/problem+json", ct)
	}
	if body["code"] != code {
		t.Errorf("code = %v, se esperaba %s", body["code"], code)
	}
}
