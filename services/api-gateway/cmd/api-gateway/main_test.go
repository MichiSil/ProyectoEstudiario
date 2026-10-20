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
)

const testSecret = "secreto-de-prueba-con-mas-de-32-caracteres"

func testRouter(t *testing.T, planner http.Handler) http.Handler {
	t.Helper()
	v, err := auth.NewVerifier(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return newRouter(v, slog.New(slog.NewJSONHandler(io.Discard, nil)), planner)
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
