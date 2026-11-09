package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/auth"
	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/ratelimit"
)

const testSecret = "secreto-de-prueba-con-mas-de-32-caracteres"

func testRouter(t *testing.T, planner http.Handler) http.Handler {
	t.Helper()
	v, err := auth.NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	limits := limiters{user: ratelimit.New(ratelimit.DefaultUser), public: ratelimit.New(ratelimit.DefaultPublic)}
	return newRouter(v, slog.New(slog.NewJSONHandler(io.Discard, nil)), limits, planner, http.NotFoundHandler(), http.NotFoundHandler())
}

func TestHealthRespondeOK(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, se esperaba application/json", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("el cuerpo no es JSON válido: %v", err)
	}
	if body.Status != "ok" || body.Service != serviceName {
		t.Errorf("cuerpo = %+v, se esperaba status ok y service %s", body, serviceName)
	}
}

func TestHealthRechazaOtrosMetodos(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter(t, http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestPlannerExigeJWT(t *testing.T) {
	var userID string
	planner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID = r.Header.Get(auth.UserIDHeader)
	})
	router := testRouter(t, planner)

	// Sin token: aunque mande X-User-Id, no llega a planner-service.
	req := httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil)
	req.Header.Set(auth.UserIDHeader, "otro-estudiante")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || userID != "" {
		t.Fatalf("sin token: status = %d, X-User-Id recibido = %q; se esperaba 401 sin llegar al servicio", rec.Code, userID)
	}

	// Con token: la identidad sale del token.
	tok, err := auth.Sign(testSecret, "estudiante-1", auth.RoleStudent, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set(auth.UserIDHeader, "otro-estudiante")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || userID != "estudiante-1" {
		t.Fatalf("con token: status = %d, X-User-Id recibido = %q; se esperaba 200 y estudiante-1", rec.Code, userID)
	}
}

func TestRegistroYLoginSonPublicosYNoAceptanIdentidadDelCliente(t *testing.T) {
	var llamadas int
	var userID string
	users := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamadas++
		userID = r.Header.Get(auth.UserIDHeader)
	})
	v, err := auth.NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	router := newRouter(v, slog.New(slog.NewJSONHandler(io.Discard, nil)), limiters{user: ratelimit.New(ratelimit.DefaultUser), public: ratelimit.New(ratelimit.DefaultPublic)}, http.NotFoundHandler(), users, http.NotFoundHandler())

	for _, path := range []string{"/api/users/register", "/api/users/login"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set(auth.UserIDHeader, "otro-usuario")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || userID != "" {
			t.Errorf("%s sin token: status = %d, X-User-Id recibido = %q; se esperaba 200 y sin identidad", path, rec.Code, userID)
		}
	}
	if llamadas != 2 {
		t.Errorf("users-service recibió %d pedidos, se esperaban 2", llamadas)
	}
}

func TestMeExigeJWTYUsaLaIdentidadDelToken(t *testing.T) {
	var userID string
	users := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID = r.Header.Get(auth.UserIDHeader)
	})
	v, err := auth.NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	router := newRouter(v, slog.New(slog.NewJSONHandler(io.Discard, nil)), limiters{user: ratelimit.New(ratelimit.DefaultUser), public: ratelimit.New(ratelimit.DefaultPublic)}, http.NotFoundHandler(), users, http.NotFoundHandler())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/me", nil))
	if rec.Code != http.StatusUnauthorized || userID != "" {
		t.Fatalf("sin token: status = %d; se esperaba 401 sin llegar a users-service", rec.Code)
	}

	tok, err := auth.Sign(testSecret, "usuario-1", auth.RoleStudent, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set(auth.UserIDHeader, "otro-usuario")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || userID != "usuario-1" {
		t.Fatalf("con token: status = %d, X-User-Id recibido = %q; se esperaba 200 y usuario-1", rec.Code, userID)
	}
}

// Control de tráfico: después de la ráfaga, el login desde una misma IP
// responde 429 y el pedido no llega a users-service; otra IP sigue pasando.
func TestLoginTieneCupoPorIP(t *testing.T) {
	v, err := auth.NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	users := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ })
	limits := limiters{user: ratelimit.New(ratelimit.DefaultUser), public: ratelimit.New(ratelimit.Rate{PerMinute: 20, Burst: 3})}
	router := newRouter(v, slog.New(slog.NewJSONHandler(io.Discard, nil)), limits, http.NotFoundHandler(), users, http.NotFoundHandler())

	login := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/users/login", nil)
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	for range 3 {
		login("190.2.3.4")
	}
	if code := login("190.2.3.4"); code != http.StatusTooManyRequests {
		t.Fatalf("cuarto login desde la misma IP: %d, quería 429", code)
	}
	if code := login("200.1.1.1"); code != http.StatusOK {
		t.Fatalf("login desde otra IP: %d", code)
	}
	if calls != 4 {
		t.Fatalf("users-service recibió %d pedidos, quería 4", calls)
	}
}
