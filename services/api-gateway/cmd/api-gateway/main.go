// Command api-gateway es el punto de entrada único del frontend de Estudiario: valida el JWT, enruta cada pedido al servicio que corresponde, aplica límites de tráfico y propaga el identificador de correlación.
//
// En la Entrega 2 expone GET /health y reenvía /api/planner/* a planner-service, con logs JSON,
// X-Correlation-Id y trazas OpenTelemetry (ver internal/observability).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/proxy"
)

const (
	serviceName       = "api-gateway"
	defaultPort       = "8080"
	defaultPlannerURL = "http://localhost:8082"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := observability.SetupTracing(ctx, serviceName)
	if err != nil {
		logger.Error("no se pudo configurar el tracing", "error", err)
		os.Exit(1)
	}

	router := newRouter(proxy.Planner(plannerTarget, logger))

	// Timeouts del servidor según el ADR-005: un cliente lento no retiene conexiones indefinidamente.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           observability.Middleware(serviceName, logger, router),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
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

func newRouter(planner http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.Handle(proxy.PlannerPrefix+"/", planner)
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
