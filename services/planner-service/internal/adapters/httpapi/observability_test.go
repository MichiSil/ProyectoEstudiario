package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/correlation"
)

// El X-Correlation-Id que recibe Observe es el mismo que leen los adaptadores
// con correlation.ID y el que sale en los logs.
func TestObserve_UnaSolaFuenteDeCorrelationID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)})

	var seen string
	h := Observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = correlation.ID(r.Context())
	}), logger)

	req := httptest.NewRequest(http.MethodGet, "/subjects", nil)
	req.Header.Set(correlation.Header, "corr-prueba")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "corr-prueba" {
		t.Fatalf("correlation.ID = %q, se esperaba corr-prueba", seen)
	}
	if got := rec.Header().Get(correlation.Header); got != "corr-prueba" {
		t.Fatalf("la respuesta devolvió %s=%q", correlation.Header, got)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("el log no es JSON: %v\n%s", err, logs.String())
	}
	if entry["correlationId"] != "corr-prueba" {
		t.Fatalf("el log no tiene el correlationId: %s", logs.String())
	}
}

// Si el pedido no trae X-Correlation-Id, se genera uno y se usa en todos lados.
func TestObserve_GeneraCorrelationID(t *testing.T) {
	var seen string
	h := Observe(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = CorrelationID(r.Context())
	}), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subjects", nil))

	if seen == "" || rec.Header().Get(correlation.Header) != seen {
		t.Fatalf("se esperaba un id generado igual en el contexto y en la respuesta: %q / %q", seen, rec.Header().Get(correlation.Header))
	}
}
