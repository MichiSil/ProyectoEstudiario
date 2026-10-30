package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
)

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestReenviaSinElPrefijoYConLosEncabezados(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var path, correlation, userID, traceparent string
	planner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		correlation = r.Header.Get(observability.CorrelationHeader)
		userID = r.Header.Get("X-User-Id")
		traceparent = r.Header.Get("traceparent")
		w.Header().Set(observability.CorrelationHeader, correlation)
		w.WriteHeader(http.StatusOK)
	}))
	defer planner.Close()

	h := observability.Middleware("api-gateway", testLogger, Planner(mustURL(t, planner.URL), testLogger))
	req := httptest.NewRequest(http.MethodPost, "/api/planner/plans/p-1/confirm", nil)
	req.Header.Set(observability.CorrelationHeader, "abc-123")
	req.Header.Set("X-User-Id", "u-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba 200", rec.Code)
	}
	if path != "/plans/p-1/confirm" {
		t.Errorf("planner-service recibió la ruta %q, se esperaba /plans/p-1/confirm", path)
	}
	if correlation != "abc-123" {
		t.Errorf("X-Correlation-Id reenviado = %q, se esperaba abc-123", correlation)
	}
	if userID != "u-1" {
		t.Errorf("X-User-Id reenviado = %q, se esperaba u-1", userID)
	}
	if traceparent == "" {
		t.Error("el pedido a planner-service no lleva traceparent")
	}
	if got := rec.Header().Values(observability.CorrelationHeader); len(got) != 1 {
		t.Errorf("la respuesta trae X-Correlation-Id %d veces (%v), se esperaba 1", len(got), got)
	}
}

func TestPlannerCaidoDevuelve503(t *testing.T) {
	planner := httptest.NewServer(http.NotFoundHandler())
	target := mustURL(t, planner.URL)
	planner.Close() // nadie escucha en esa dirección

	rec := httptest.NewRecorder()
	Planner(target, testLogger).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil))

	assertProblem(t, rec, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE")
}

func TestPlannerLentoDevuelve504(t *testing.T) {
	planner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer planner.Close()

	h := Planner(mustURL(t, planner.URL), testLogger)
	req := httptest.NewRequest(http.MethodGet, "/api/planner/sessions", nil)
	// Un deadline corto simula que se cumplió el timeout del gateway.
	ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(ctx))

	assertProblem(t, rec, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT")
}

func TestTimeoutSegunLaOperacion(t *testing.T) {
	casos := []struct {
		method, path string
		want         time.Duration
	}{
		{http.MethodPost, "/api/planner/exams/e-1/proposals", ProposalTimeout},
		{http.MethodPost, "/api/planner/plans/p-1/confirm", DefaultTimeout},
		{http.MethodGet, "/api/planner/sessions", DefaultTimeout},
	}
	for _, c := range casos {
		if got := timeoutFor(httptest.NewRequest(c.method, c.path, nil)); got != c.want {
			t.Errorf("%s %s: timeout = %v, se esperaba %v", c.method, c.path, got, c.want)
		}
	}
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, status)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, se esperaba application/problem+json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("el cuerpo no es JSON: %v", err)
	}
	if body["code"] != code {
		t.Errorf("code = %v, se esperaba %s", body["code"], code)
	}
}

func TestUsersReenviaSinElPrefijo(t *testing.T) {
	var path string
	users := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
	}))
	defer users.Close()

	rec := httptest.NewRecorder()
	Users(mustURL(t, users.URL), testLogger).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/users/login", nil))

	if rec.Code != http.StatusOK || path != "/login" {
		t.Fatalf("status = %d, ruta recibida = %q; se esperaba 200 y /login", rec.Code, path)
	}
}

func TestUsersCaidoDevuelve503(t *testing.T) {
	users := httptest.NewServer(http.NotFoundHandler())
	target := mustURL(t, users.URL)
	users.Close()

	rec := httptest.NewRecorder()
	Users(target, testLogger).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/users/login", nil))

	assertProblem(t, rec, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE")
}
