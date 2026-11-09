package observability

// Métricas de Prometheus (ADR-011): los pedidos HTTP por ruta, método y
// estado, con un histograma de duración, y el servidor de GET /metrics.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// DefaultMetricsAddr es donde se expone GET /metrics si no se define
// METRICS_ADDR: un puerto aparte del de la API, que no se publica.
const DefaultMetricsAddr = ":9100"

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Pedidos HTTP atendidos, por ruta registrada, método y estado (ADR-011).",
	}, []string{"route", "method", "status"})
	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "Duración de los pedidos HTTP, por ruta registrada y método (ADR-011).",
		// Incluye 0,5 s y 10 s, los umbrales del SLO, y 20 s, la subida de un PDF.
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20},
	}, []string{"route", "method"})
)

// route devuelve la ruta registrada del pedido (el patrón del ServeMux, por
// ejemplo /summaries/{id}), sin el método. Se usa el patrón y no la URL para no
// crear una serie por cada id. Un pedido que no coincidió con ninguna ruta
// cuenta como "sin_ruta".
func route(r *http.Request) string {
	p := r.Pattern
	if _, path, ok := strings.Cut(p, " "); ok {
		p = path
	}
	if p == "" {
		return "sin_ruta"
	}
	return p
}

// observeRequest cuenta un pedido ya atendido. r tiene que ser el pedido que
// recibió el ServeMux, que es el que guarda el patrón. GET /health no se
// cuenta: lo consultan los healthchecks cada pocos segundos y taparía el
// tráfico real.
func observeRequest(r *http.Request, status int, d time.Duration) {
	rt := route(r)
	if rt == "/health" {
		return
	}
	httpRequests.WithLabelValues(rt, r.Method, strconv.Itoa(status)).Inc()
	httpDuration.WithLabelValues(rt, r.Method).Observe(d.Seconds())
}

// ServeMetrics expone GET /metrics en addr, en un servidor aparte del de la
// API: así no se puede llegar a /metrics a través del api-gateway ni de Caddy,
// y en la nube su puerto no se publica. Corre hasta que se cancele ctx.
func ServeMetrics(ctx context.Context, addr string, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	logger.Info("métricas expuestas en GET /metrics", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("no se pudo exponer /metrics", "error", err.Error())
	}
}
