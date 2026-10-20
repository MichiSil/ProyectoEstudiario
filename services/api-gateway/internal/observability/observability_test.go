package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(contextHandler{Handler: slog.NewJSONHandler(buf, nil)}).With("service", "api-gateway")
}

func TestMiddlewareGeneraElCorrelationIdSiNoViene(t *testing.T) {
	var buf bytes.Buffer
	var visto string
	h := Middleware("api-gateway", newTestLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visto = CorrelationID(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/planner/subjects", nil))

	id := rec.Header().Get(CorrelationHeader)
	if id == "" {
		t.Fatal("la respuesta no trae X-Correlation-Id")
	}
	if visto != id {
		t.Errorf("el handler vio %q y la respuesta devolvió %q", visto, id)
	}
}

func TestMiddlewareRespetaElCorrelationIdRecibido(t *testing.T) {
	var buf bytes.Buffer
	var reenviado string
	h := Middleware("api-gateway", newTestLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reenviado = r.Header.Get(CorrelationHeader)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/planner/subjects", nil)
	req.Header.Set(CorrelationHeader, "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get(CorrelationHeader); got != "abc-123" {
		t.Errorf("X-Correlation-Id de la respuesta = %q, se esperaba abc-123", got)
	}
	if reenviado != "abc-123" {
		t.Errorf("el pedido que sigue lleva %q, se esperaba abc-123", reenviado)
	}
}

func TestMiddlewareLogueaElPedidoEnJSON(t *testing.T) {
	var buf bytes.Buffer
	h := Middleware("api-gateway", newTestLogger(&buf), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/planner/subjects", nil)
	req.Header.Set(CorrelationHeader, "abc-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	var linea map[string]any
	if err := json.Unmarshal(buf.Bytes(), &linea); err != nil {
		t.Fatalf("el log no es una línea JSON: %v\n%s", err, buf.String())
	}
	esperado := map[string]any{
		"level":         "INFO",
		"service":       "api-gateway",
		"correlationId": "abc-123",
		"method":        "POST",
		"path":          "/api/planner/subjects",
		"status":        float64(http.StatusCreated),
	}
	for campo, valor := range esperado {
		if linea[campo] != valor {
			t.Errorf("%s = %v, se esperaba %v", campo, linea[campo], valor)
		}
	}
	for _, campo := range []string{"time", "durationMs"} {
		if _, ok := linea[campo]; !ok {
			t.Errorf("falta el campo %s en el log", campo)
		}
	}
}

func TestMiddlewareNoLogueaElHealth(t *testing.T) {
	var buf bytes.Buffer
	h := Middleware("api-gateway", newTestLogger(&buf), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if buf.Len() != 0 {
		t.Errorf("no se esperaba log para /health, se obtuvo: %s", buf.String())
	}
}
