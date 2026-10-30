// Command forum-service administra el foro de resúmenes: publicación,
// moderación, calificaciones y búsqueda (SPEC 7), y expone la capacidad de
// registro y consulta de documentos para otros grupos (SPEC 11.1, ADR-014).
// Está organizado por capas: controller → service → repository (ADR-002).
//
// Con el argumento "reindex" no levanta el servidor: vacía el índice de Solr y
// lo vuelve a armar desde MongoDB, y termina (ADR-006, 4).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/apikey"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/cleanup"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/controller"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/observability"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/ratelimit"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/relay"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/repository"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/search"
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

	// Búsqueda sobre Solr (ADR-006), con el indexador corriendo aparte. Sin
	// SOLR_URL (por ejemplo, al probar sin Docker) se usa la de MongoDB.
	var summarySearch service.SummarySearch = repository.NewSearch(db)
	if solrURL := os.Getenv("SOLR_URL"); solrURL != "" {
		solr := search.NewSolr(solrURL)
		indexer := &search.Indexer{Solr: solr, Source: repo, Logger: logger}
		if len(os.Args) > 1 && os.Args[1] == "reindex" {
			started := time.Now()
			n, err := indexer.Rebuild(ctx)
			if err != nil {
				return err
			}
			logger.Info("índice reconstruido", "documents", n, "durationMs", time.Since(started).Milliseconds())
			return nil
		}
		summarySearch = &search.SolrSearch{Solr: solr, Loader: repo}
		go indexer.Run(ctx)
		logger.Info("búsqueda sobre Solr", "url", solrURL)
	} else if len(os.Args) > 1 && os.Args[1] == "reindex" {
		return errors.New("reindex necesita SOLR_URL")
	} else {
		logger.Warn("sin SOLR_URL: la búsqueda usa MongoDB")
	}
	svc := &service.Service{Repo: repo, Search: summarySearch, Files: files}

	// Limpieza periódica de PDF huérfanos en MinIO (ADR-003).
	cleanInterval, err := durationEnv("FORUM_CLEANUP_INTERVAL", cleanup.DefaultInterval)
	if err != nil {
		return err
	}
	cleanMinAge, err := durationEnv("FORUM_CLEANUP_MIN_AGE", cleanup.DefaultMinAge)
	if err != nil {
		return err
	}
	go (&cleanup.Cleaner{Files: files, Docs: repo, Logger: logger, Interval: cleanInterval, MinAge: cleanMinAge}).Run(ctx)
	logger.Info("limpieza de PDF huérfanos programada", "intervalMs", cleanInterval.Milliseconds(), "minAgeMs", cleanMinAge.Milliseconds())

	// Relay del outbox a RabbitMQ (ADR-005, 3.3). Corre aparte: si el broker
	// está caído, la moderación sigue y los eventos esperan en el outbox (RN-33).
	if amqpURL := os.Getenv("RABBITMQ_URL"); amqpURL != "" {
		publisher := &relay.RabbitMQ{URL: amqpURL, Exchange: relay.Exchange}
		defer publisher.Close()
		go (&relay.Relay{Outbox: repo, Publisher: publisher, Logger: logger}).Run(ctx)
		logger.Info("relay del outbox a RabbitMQ iniciado", "exchange", relay.Exchange)
	} else {
		logger.Warn("sin RABBITMQ_URL: los eventos del outbox no se publican")
	}

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

// durationEnv lee una duración de una variable de entorno (por ejemplo "1h" o
// "30s"), con un valor por defecto si no está.
func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%s tiene que ser una duración como 1h o 30s: %q", key, v)
	}
	return d, nil
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
