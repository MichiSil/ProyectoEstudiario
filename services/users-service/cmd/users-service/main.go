// Command users-service administra el registro, el inicio de sesión, la emisión
// del JWT y los roles de Estudiario (SPEC, sección 5; ADR-001). Está organizado
// por capas: controller → service → repository (ADR-002).
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

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/controller"
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
	h := &controller.Handler{Svc: svc, DB: repo, Logger: logger, InternalToken: internalToken}

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
