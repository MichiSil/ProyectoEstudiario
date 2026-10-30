// Command api-gateway es el punto de entrada único del frontend de Estudiario: valida el JWT, enruta cada pedido al servicio que corresponde, aplica límites de tráfico y propaga el identificador de correlación.
//
// Expone GET /health y reenvía /api/users/* a users-service, /api/planner/* a planner-service y
// /api/forum/* a forum-service,
// con logs JSON, X-Correlation-Id y trazas OpenTelemetry (ver internal/observability).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/auth"
	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/proxy"
)

const (
	serviceName       = "api-gateway"
	defaultPort       = "8080"
	defaultPlannerURL = "http://localhost:8082"
	defaultUsersURL   = "http://localhost:8081"
	defaultForumURL   = "http://localhost:8083"
)

func main() {
	logger := observability.NewLogger(serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	plannerURL := os.Getenv("PLANNER_SERVICE_URL")
	if plannerURL == "" {
		plannerURL = defaultPlannerURL
	}
	plannerTarget, err := url.Parse(plannerURL)
	if err != nil {
		logger.Error("PLANNER_SERVICE_URL no es una URL válida", "error", err)
		os.Exit(1)
	}

	usersURL := os.Getenv("USERS_SERVICE_URL")
	if usersURL == "" {
		usersURL = defaultUsersURL
	}
	usersTarget, err := url.Parse(usersURL)
	if err != nil {
		logger.Error("USERS_SERVICE_URL no es una URL válida", "error", err)
		os.Exit(1)
	}

	// forum-service corre en varias instancias (ADR-012): FORUM_SERVICE_URLS
	// las lista separadas por comas. FORUM_SERVICE_URL sigue sirviendo para una sola.
	forumURLs := os.Getenv("FORUM_SERVICE_URLS")
	if forumURLs == "" {
		forumURLs = os.Getenv("FORUM_SERVICE_URL")
	}
	if forumURLs == "" {
		forumURLs = defaultForumURL
	}
	var forumTargets []*url.URL
	for _, raw := range strings.Split(forumURLs, ",") {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" {
			logger.Error("FORUM_SERVICE_URLS tiene una URL inválida", "url", raw)
			os.Exit(1)
		}
		forumTargets = append(forumTargets, u)
	}
	forumBalancer := proxy.NewBalancer("forum-service", forumTargets, logger)
	logger.Info("instancias de forum-service", "count", len(forumTargets))

	// Los JWT los firma users-service con este secreto compartido (HS256).
	// Sin un secreto válido el gateway no arranca: nunca acepta pedidos sin validar.
	verifier, err := auth.NewVerifier(os.Getenv("JWT_SECRET"))
	if err != nil {
		logger.Error("no se puede validar el JWT", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Chequeo de salud de las instancias del foro hasta que se apague el gateway.
	go forumBalancer.Run(ctx)

	shutdownTracing, err := observability.SetupTracing(ctx, serviceName)
	if err != nil {
		logger.Error("no se pudo configurar el tracing", "error", err)
		os.Exit(1)
	}

	// planner-service y users-service sólo aceptan X-User-Id si el pedido trae el secreto interno.
	planner, err := auth.WithInternalToken(os.Getenv("INTERNAL_TOKEN"), proxy.Planner(plannerTarget, logger))
	if err != nil {
		logger.Error("no se puede llamar a los servicios internos", "error", err)
		os.Exit(1)
	}
	users, err := auth.WithInternalToken(os.Getenv("INTERNAL_TOKEN"), proxy.Users(usersTarget, logger))
	if err != nil {
		logger.Error("no se puede llamar a los servicios internos", "error", err)
		os.Exit(1)
	}

	forum, err := auth.WithInternalToken(os.Getenv("INTERNAL_TOKEN"), proxy.Forum(forumBalancer, logger))
	if err != nil {
		logger.Error("no se puede llamar a los servicios internos", "error", err)
		os.Exit(1)
	}

	router := newRouter(verifier, logger, planner, users, forum)

	// Timeouts del servidor según el ADR-005: un cliente lento no retiene conexiones indefinidamente.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           observability.Middleware(serviceName, logger, router),
		ReadHeaderTimeout: 5 * time.Second,
		// 25 s: subir un PDF de 10 MB al foro puede tardar hasta 20 s (ADR-005).
		ReadTimeout:  25 * time.Second,
		WriteTimeout: 25 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("servidor iniciado", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("el servidor se detuvo por un error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("apagando el servidor")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("no se pudo apagar el servidor de forma ordenada", "error", err)
		os.Exit(1)
	}
	if err := shutdownTracing(shutdownCtx); err != nil {
		logger.Error("no se pudieron enviar las últimas trazas", "error", err)
	}
	logger.Info("servidor detenido")
}

// newRouter arma las rutas. Registrarse e iniciar sesión son públicos (todavía
// no hay token); el resto de /api/users/* y todo /api/planner/* exigen un JWT
// válido, igual que /api/forum/*. En las rutas públicas igual se descarta la
// identidad que mande el cliente.
func newRouter(verifier *auth.Verifier, logger *slog.Logger, planner, users, forum http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle("POST "+proxy.UsersPrefix+"/register", auth.StripIdentity(users))
	mux.Handle("POST "+proxy.UsersPrefix+"/login", auth.StripIdentity(users))
	mux.Handle(proxy.UsersPrefix+"/", auth.Require(verifier, logger, users))
	mux.Handle(proxy.PlannerPrefix+"/", auth.Require(verifier, logger, planner))
	mux.Handle(proxy.ForumPrefix+"/", auth.Require(verifier, logger, forum))
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
