// Package observability reúne la configuración de logs y trazas del api-gateway:
// logs estructurados en JSON con log/slog, el identificador de correlación
// (X-Correlation-Id) y tracing con OpenTelemetry exportado por OTLP a Jaeger.
// Ver docs/adr/ADR-011-observabilidad.md.
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

// CorrelationHeader es el encabezado con el que viaja el identificador de
// correlación entre el cliente, el gateway y los servicios (ADR-005, 2.5).
const CorrelationHeader = "X-Correlation-Id"

type correlationKey struct{}

// WithCorrelationID guarda el identificador de correlación en el contexto.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID devuelve el identificador de correlación del pedido, o "" si no hay.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

// NewLogger crea el logger JSON del servicio. Cada línea lleva time, level,
// msg y service, y además el correlationId cuando el log se emite con el
// contexto de un pedido (logger.InfoContext, logger.Log, etc.).
func NewLogger(service string) *slog.Logger {
	handler := contextHandler{Handler: slog.NewJSONHandler(os.Stdout, nil)}
	return slog.New(handler).With("service", service)
}

// contextHandler agrega el correlationId del contexto a cada registro.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlationId", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

// SetupTracing configura OpenTelemetry: propagación W3C (traceparent) y, si
// está definida OTEL_EXPORTER_OTLP_ENDPOINT, exportación de las trazas por
// OTLP/HTTP (en local, a Jaeger). Devuelve la función que vacía y cierra el
// exportador al apagar el servicio.
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

// Middleware envuelve el router con:
//   - tracing: abre un span por pedido, o continúa la traza si el pedido trae traceparent;
//   - correlación: genera el X-Correlation-Id si no viene, lo guarda en el
//     contexto y lo devuelve en la respuesta;
//   - un log JSON por pedido con method, path, status y durationMs.
//
// GET /health no se loguea: lo consultan los healthchecks cada pocos segundos.
func Middleware(service string, logger *slog.Logger, next http.Handler) http.Handler {
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(CorrelationHeader))
		if id == "" {
			id = strings.ToLower(rand.Text())
		}
		r.Header.Set(CorrelationHeader, id)
		w.Header().Set(CorrelationHeader, id)

		ctx := WithCorrelationID(r.Context(), id)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("correlation.id", id))

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
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", time.Since(start).Milliseconds())
	})

	return otelhttp.NewHandler(logged, service,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }))
}

// statusRecorder guarda el código de estado que escribe el handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
