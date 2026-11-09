// Command users-service administra el registro, el inicio de sesión, la emisión
// del JWT y los roles de Estudiario (SPEC, sección 5; ADR-001), la vinculación
// con el Club de Beneficios del Grupo 1 (RF-28, ADR-009) y las cuentas de los
// autores de la agencia de viajes (ADR-014). Está organizado por capas:
// controller → service → repository (ADR-002).
package main

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/broker"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/controller"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/mail"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/observability"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/repository"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/token"
)

const serviceName = "users-service"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := observability.NewLogger(serviceName, env("INSTANCE", env("HOSTNAME", "local")))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("el servicio terminó con error", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := observability.SetupTracing(ctx, serviceName)
	if err != nil {
		return err
	}
	defer shutdownTracing(context.Background()) //nolint:errcheck

	// Mismo secreto con el que el api-gateway valida los JWT.
	signer, err := token.NewSigner(os.Getenv("JWT_SECRET"), token.DefaultTTL)
	if err != nil {
		return err
	}
	// Secreto compartido con el api-gateway: sin él, GET /me no acepta X-User-Id.
	internalToken := os.Getenv("INTERNAL_TOKEN")
	if len(internalToken) < 32 {
		return errors.New("INTERNAL_TOKEN falta o tiene menos de 32 caracteres")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("falta la variable DATABASE_URL")
	}
	pool, err := connect(ctx, dbURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := repository.New(pool)
	if err := repo.Migrate(ctx); err != nil {
		return err
	}

	admins := service.ParseAdminEmails(os.Getenv("ADMIN_EMAILS"))
	logger.Info("mails con rol admin configurados", "count", len(admins))
	svc := &service.Service{Repo: repo, Tokens: signer, AdminEmails: admins}

	// Club de Beneficios del Grupo 1 (RF-28, ADR-009). La API key va sólo en el
	// .env: sin ella, el perfil muestra el aviso CLUB_UNAVAILABLE y los puntos
	// quedan pendientes hasta que se configure.
	clubURL := env("CLUB_BASE_URL", "https://metalfitness-club.vercel.app/partner-api")
	if os.Getenv("CLUB_API_KEY") == "" {
		logger.Warn("falta CLUB_API_KEY: no se puede llamar al Club de Beneficios")
	}
	clubClient := club.New(clubURL, os.Getenv("CLUB_API_KEY"), logger)
	clubClient.CorrelationID = observability.CorrelationID
	clubSvc := &service.Club{Repo: repo, Provider: clubClient, Logger: logger}
	logger.Info("Club de Beneficios configurado", "baseUrl", clubURL)
	go clubSvc.RunSweeper(ctx)

	// Métricas del Club (ADR-011, ADR-015): el estado del circuit breaker y los
	// puntos pendientes de acreditar, que se leen de la base en cada scrape.
	prometheus.MustRegister(clubClient.BreakerGauge(), prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "users_club_pending_points",
		Help: "Puntos del Club de Beneficios guardados y todavía sin acreditar (RN-43).",
	}, func() float64 {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		n, err := repo.PendingClubPoints(ctx)
		if err != nil {
			return math.NaN() // sin dato: el tablero lo muestra vacío, no en 0
		}
		return float64(n)
	}))
	// GET /metrics en un puerto aparte, que no se publica (ADR-011).
	go observability.ServeMetrics(ctx, env("METRICS_ADDR", observability.DefaultMetricsAddr), logger)

	// Eventos de forum-service por RabbitMQ (ADR-005): puntos por resumen
	// aprobado y cuentas de los autores de la agencia de viajes.
	accounts := &service.Accounts{
		Repo:   repo,
		Mailer: &mail.SMTP{Addr: env("SMTP_ADDR", "mailpit:1025"), From: env("MAIL_FROM", "Estudiario <no-reply@estudiario.local>")},
		Logger: logger,
		AppURL: env("APP_URL", "http://localhost:5173"),
	}
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		go (&broker.Consumer{
			URL: rabbitURL, Logger: logger,
			Handler: &consumer.Benefits{Club: clubSvc, Logger: logger},
			Queue:   broker.BenefitsQueue, RetryQueue: broker.BenefitsRetryQueue, DLQ: broker.BenefitsDLQ,
		}).Run(ctx)
		go (&broker.Consumer{
			URL: rabbitURL, Logger: logger,
			Handler: &consumer.ExternalAuthors{Accounts: accounts, Logger: logger},
			Queue:   broker.ExternalAuthorsQueue, RetryQueue: broker.ExternalAuthorsRetryQueue, DLQ: broker.ExternalAuthorsDLQ,
		}).Run(ctx)
	} else {
		logger.Warn("falta RABBITMQ_URL: no se consumen los eventos del foro")
	}

	h := &controller.Handler{Svc: svc, Club: clubSvc, DB: repo, Logger: logger, InternalToken: internalToken}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8081"),
		Handler:           observability.Middleware(serviceName, logger, h.Routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logger.Info("users-service escuchando", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// connect abre el pool y reintenta con espera exponencial mientras PostgreSQL
// arranca, igual que planner-service.
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
