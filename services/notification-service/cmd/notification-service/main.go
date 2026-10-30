// Command notification-service consume los eventos de moderación del foro
// (summary.approved y summary.rejected) y le manda el mail al autor
// exactamente una vez (RF-23, RF-24, RN-32 a RN-34, ADR-005).
//
//	RabbitMQ (notifications.summary-moderation) → consumer → MongoDB (sent_notifications) + SMTP
//
// Expone GET /health.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/broker"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/mail"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/repository"
)

const (
	serviceName = "notification-service"
	defaultPort = "8084"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// Logs JSON con service e instance; cada mensaje agrega eventId y correlationId (ADR-011).
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With("service", serviceName, "instance", env("INSTANCE", env("HOSTNAME", "local")))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("el servicio terminó con error", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		return errors.New("falta la variable MONGO_URI")
	}
	amqpURL := os.Getenv("RABBITMQ_URL")
	if amqpURL == "" {
		return errors.New("falta la variable RABBITMQ_URL")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background()) //nolint:errcheck
	store := repository.New(client.Database("notifications"))
	if err := retry(ctx, logger, "MongoDB", store.Ping); err != nil {
		return err
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		return err
	}

	mailer := &mail.SMTP{
		Addr: env("SMTP_ADDR", "localhost:1025"),
		From: env("MAIL_FROM", "Estudiario <no-reply@estudiario.local>"),
	}
	handler := &consumer.Handler{Store: store, Mailer: mailer, Logger: logger}
	// Si RabbitMQ no está, el consumidor espera y reintenta: el servicio arranca igual.
	go (&broker.Consumer{URL: amqpURL, Handler: handler, Logger: logger}).Run(ctx)
	logger.Info("notification-service listo", "smtp", mailer.Addr, "queue", broker.Queue)

	port := env("PORT", defaultPort)
	// Timeouts del servidor según el ADR-005: un cliente lento no retiene conexiones indefinidamente.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logger.Info("servidor iniciado", "port", port)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// retry espera a que una dependencia responda, con espera exponencial, como
// los demás servicios al arrancar.
func retry(ctx context.Context, logger *slog.Logger, name string, ping func(context.Context) error) error {
	wait := time.Second
	for attempt := 1; ; attempt++ {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		if attempt == 6 {
			return err
		}
		logger.Warn(name+" no disponible, reintentando", "attempt", attempt, "waitMs", wait.Milliseconds(), "error", err.Error())
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
		wait *= 2
	}
}

func newRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	return mux
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Service: serviceName})
}
