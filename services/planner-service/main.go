// Command planner-service administra materias, calendario, disponibilidad y el plan de estudio de Estudiario, y expone la capacidad publicada para otros grupos (docs/contracts/planner-v1.yaml).
//
// Por ahora sólo expone GET /health. La funcionalidad se agrega a partir de la Entrega 2.
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
)

const (
	serviceName = "planner-service"
	defaultPort = "8082"

	// contractVersion es la versión vigente del contrato publicado (docs/contracts/planner-v1.yaml).
	contractVersion = "1.0.0"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	// Timeouts del servidor según el ADR-005: un cliente lento no retiene conexiones indefinidamente.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	logger.Info("servidor detenido")
}

func newRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	return mux
}

type healthResponse struct {
	Status          string `json:"status"`
	Service         string `json:"service"`
	ContractVersion string `json:"contractVersion"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Service: serviceName, ContractVersion: contractVersion})
}
