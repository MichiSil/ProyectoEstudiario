package httpapi

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ctxKey struct{}

// CorrelationID devuelve el identificador de correlación del pedido.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// ContextHandler agrega a cada log el correlationId y el traceId del pedido,
// así un mismo pedido se puede seguir en los logs de todos los servicios y en
// la traza de Jaeger.
type ContextHandler struct{ slog.Handler }

func (h ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlationId", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("traceId", sc.TraceID().String()), slog.String("spanId", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h ContextHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return ContextHandler{h.Handler.WithAttrs(a)}
}
func (h ContextHandler) WithGroup(n string) slog.Handler {
	return ContextHandler{h.Handler.WithGroup(n)}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Observe envuelve el router con: propagación del X-Correlation-Id (lo genera
// si no viene), tracing OpenTelemetry (continúa la traza del gateway vía
// traceparent) y un log estructurado por pedido.
func Observe(next http.Handler, logger *slog.Logger) http.Handler {
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
		if id == "" {
			id = strings.ToLower(rand.Text())
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, id)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("correlation.id", id))
		w.Header().Set("X-Correlation-Id", id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		}
		if r.URL.Path != "/health" {
			logger.Log(ctx, level, "request",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"durationMs", time.Since(start).Milliseconds())
		}
	})
	return otelhttp.NewHandler(logged, "planner-service",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }))
}
