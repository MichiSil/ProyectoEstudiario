package observability

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Las métricas usan la ruta registrada, no la URL con el id, y no cuentan
// GET /health (ADR-011).
func TestMetricasPorRutaRegistrada(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /metricas-test/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	h := Middleware("forum-service", slog.New(slog.NewJSONHandler(io.Discard, nil)), mux)

	before := testutil.ToFloat64(httpRequests.WithLabelValues("/metricas-test/{id}", "GET", "404"))
	for _, id := range []string{"a1", "b2", "c3"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metricas-test/"+id, nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no-existe", nil))

	if got := testutil.ToFloat64(httpRequests.WithLabelValues("/metricas-test/{id}", "GET", "404")) - before; got != 3 {
		t.Errorf("pedidos a /metricas-test/{id} = %v, se esperaban 3", got)
	}
	if got := testutil.ToFloat64(httpRequests.WithLabelValues("/health", "GET", "200")); got != 0 {
		t.Errorf("se contó /health: %v", got)
	}
	if got := testutil.ToFloat64(httpRequests.WithLabelValues("sin_ruta", "GET", "404")); got < 1 {
		t.Errorf("un pedido sin ruta no se contó como sin_ruta: %v", got)
	}
}
