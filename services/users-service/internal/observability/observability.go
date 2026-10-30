// Package observability tiene los logs JSON, la correlación y el tracing de
// users-service, con el mismo formato que el api-gateway y planner-service
// (ADR-011).
package observability

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// CorrelationHeader transporta el identificador de correlación (ADR-005, 2.5).
const CorrelationHeader = "X-Correlation-Id"

type correlationKey struct{}

// CorrelationID devuelve el identificador de correlación del pedido, o "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

// NewLogger crea el logger JSON del servicio, con service e instance fijos.
func NewLogger(service, instance string) *slog.Logger {
	return slog.New(contextHandler{slog.NewJSONHandler(os.Stdout, nil)}).
		With("service", service, "instance", instance)
}

// contextHandler agrega a cada log el correlationId y el traceId del pedido.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlationId", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("traceId", sc.TraceID().String()), slog.String("spanId", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(a)}
}

func (h contextHandler) WithGroup(n string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(n)}
}

// SetupTracing exporta las trazas por OTLP/HTTP (a Jaeger) si está definida
// OTEL_EXPORTER_OTLP_ENDPOINT; si no, las trazas no se exportan.
func SetupTracing(ctx context.Context, service string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(semconv.ServiceName(service)))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Middleware propaga el X-Correlation-Id (lo genera si no viene), continúa la
// traza del gateway (traceparent) y escribe un log JSON por pedido. Nunca
// loguea el cuerpo: en el registro y el login trae la contraseña.
func Middleware(service string, logger *slog.Logger, next http.Handler) http.Handler {
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(CorrelationHeader))
		if id == "" {
			id = strings.ToLower(rand.Text())
		}
		ctx := context.WithValue(r.Context(), correlationKey{}, id)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("correlation.id", id))
		w.Header().Set(CorrelationHeader, id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))

		if r.URL.Path == "/health" {
			return
		}
		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		logger.Log(ctx, level, "request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"durationMs", time.Since(start).Milliseconds())
	})
	return otelhttp.NewHandler(logged, service,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }))
}
