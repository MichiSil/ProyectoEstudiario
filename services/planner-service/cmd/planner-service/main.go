// planner-service: materias, exámenes, disponibilidad y plan de estudio.
// Es el servicio de la acción principal (confirmar el plan). Ver docs/ARCHITECTURE.md.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/httpapi"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/postgres"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/rules"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/system"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
)

const serviceName = "planner-service"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := slog.New(httpapi.ContextHandler{Handler: slog.NewJSONHandler(os.Stdout, nil)}).
		With("service", serviceName, "instance", env("HOSTNAME", "local"))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("el servicio terminó con error", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := setupTracing(ctx)
	if err != nil {
		return err
	}
	defer shutdownTracing(context.Background()) //nolint:errcheck

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("falta la variable DATABASE_URL")
	}
	pool, err := connect(ctx, dbURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := postgres.New(pool, system.IDs{})
	if err := repo.Migrate(ctx); err != nil {
		return err
	}

	// El generador es el motor de reglas. El adaptador de IA (adapters/ai) lo
	// envuelve con fallback cuando está configurado (RN-09).
	var generator app.PlanGenerator = rules.Engine{}

	svc := &app.Service{Repo: repo, Generator: generator, Clock: system.Clock{}, IDs: system.IDs{}}
	h := &httpapi.Handler{Svc: svc, DB: repo, Logger: logger}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8082"),
		Handler:           httpapi.Observe(h.Routes(), logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logger.Info("planner-service escuchando", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// connect abre el pool y reintenta con espera exponencial mientras PostgreSQL
// arranca (como en la demo de backoff de la clase 4).
func connect(ctx context.Context, url string, logger *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	wait := time.Second
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if attempt == 6 {
			pool.Close()
			return nil, err
		}
		logger.Warn("PostgreSQL no disponible, reintentando", "attempt", attempt, "waitMs", wait.Milliseconds(), "error", err.Error())
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		}
		wait *= 2
	}
}

// setupTracing exporta las trazas por OTLP/HTTP (a Jaeger) si está definida
// OTEL_EXPORTER_OTLP_ENDPOINT; si no, las trazas no se exportan.
func setupTracing(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}
