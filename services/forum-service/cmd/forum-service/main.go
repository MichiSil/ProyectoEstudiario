// Command forum-service administra el foro de resúmenes: publicación,
// moderación, calificaciones y búsqueda (SPEC 7), y expone la capacidad de
// registro y consulta de documentos para otros grupos (SPEC 11.1, ADR-014).
// Está organizado por capas: controller → service → repository (ADR-002).
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

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/apikey"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/controller"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/observability"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/ratelimit"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/repository"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/storage"
)

const serviceName = "forum-service"

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

	// Secreto compartido con el api-gateway: sin él, la API del foro no acepta
	// la identidad de X-User-Id.
	internalToken := os.Getenv("INTERNAL_TOKEN")
	if len(internalToken) < 32 {
		return errors.New("INTERNAL_TOKEN falta o tiene menos de 32 caracteres")
	}

	// API keys de los sistemas que consumen la capacidad (ADR-014): sólo sus hashes.
	keys, err := apikey.Parse(os.Getenv("FORUM_API_KEYS"))
	if err != nil {
		return err
	}
	logger.Info("API keys de la capacidad configuradas", "count", keys.Len())

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		return errors.New("falta la variable MONGO_URI")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background()) //nolint:errcheck
	db := client.Database("forum")
	repo := repository.New(db)
	if err := retry(ctx, logger, "MongoDB", repo.Ping); err != nil {
		return err
	}
	if err := repo.EnsureIndexes(ctx); err != nil {
		return err
	}

	files, err := storage.NewMinIO(env("MINIO_ENDPOINT", "localhost:9000"), os.Getenv("MINIO_ACCESS_KEY"),
		os.Getenv("MINIO_SECRET_KEY"), env("MINIO_BUCKET", "summaries"), os.Getenv("MINIO_SECURE") == "true")
	if err != nil {
		return err
	}
	if err := retry(ctx, logger, "MinIO", files.Ping); err != nil {
		return err
	}

	// La búsqueda queda detrás de SummarySearch: hoy sobre MongoDB, después
	// sobre Solr (ADR-006).
	svc := &service.Service{Repo: repo, Search: repository.NewSearch(db), Files: files}

	mux := http.NewServeMux()
	mux.Handle("GET /health", controller.Health(repo))
	forum := &controller.Forum{Svc: svc, DB: repo, Logger: logger, InternalToken: internalToken}
	forum.Register(mux)
	v1 := &controller.V1{Svc: svc, Keys: keys, Limiter: ratelimit.New(), Logger: logger}
	v1.Register(mux)

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8083"),
		Handler:           observability.Middleware(serviceName, logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
		// Subir un PDF de 10 MB puede tardar más que una lectura común (ADR-005: 20 s en el gateway).
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logger.Info("forum-service escuchando", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// retry espera a que una dependencia responda, con espera exponencial, como
// planner-service y users-service con PostgreSQL.
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
