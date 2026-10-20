// Package proxy reenvía los pedidos del frontend a los servicios internos.
package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
)

// PlannerPrefix es la ruta pública de planner-service en el gateway.
const PlannerPrefix = "/api/planner"

// Timeouts del gateway hacia planner-service (ADR-005, 2.1).
const (
	DefaultTimeout  = 5 * time.Second
	ProposalTimeout = 12 * time.Second
)

// Planner devuelve un reverse proxy hacia planner-service: /api/planner/<ruta>
// se reenvía como /<ruta>. Cada pedido tiene un timeout, lleva el
// X-Correlation-Id y propaga la traza con el encabezado traceparent (W3C).
func Planner(target *url.URL, logger *slog.Logger) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = stripPrefix(pr.In.URL.Path)
			pr.Out.URL.RawPath = ""
			pr.SetXForwarded()
		},
		// otelhttp.NewTransport inyecta traceparent en el pedido saliente.
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		// El gateway ya devuelve el X-Correlation-Id (el mismo que reenvió):
		// se saca el del servicio para que la respuesta no lo traiga repetido.
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del(observability.CorrelationHeader)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			status, code, title := http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "planner-service no está disponible"
			if errors.Is(err, context.DeadlineExceeded) {
				status, code, title = http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "planner-service no respondió a tiempo"
			}
			logger.ErrorContext(r.Context(), "falla al llamar a planner-service", "error", err.Error())
			writeProblem(w, r, status, code, title)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeoutFor(r))
		defer cancel()
		rp.ServeHTTP(w, r.WithContext(ctx))
	})
}

func stripPrefix(path string) string {
	p := strings.TrimPrefix(path, PlannerPrefix)
	if p == "" {
		return "/"
	}
	return p
}

// timeoutFor da 12 s a la propuesta de plan (puede esperar a la IA) y 5 s al resto.
func timeoutFor(r *http.Request) time.Duration {
	p := stripPrefix(r.URL.Path)
	if r.Method == http.MethodPost && strings.HasPrefix(p, "/exams/") && strings.HasSuffix(p, "/proposals") {
		return ProposalTimeout
	}
	return DefaultTimeout
}

// writeProblem responde en formato Problem Details (RFC 9457), como pide el ADR-005 (2.2).
func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":          "about:blank",
		"title":         title,
		"status":        status,
		"code":          code,
		"correlationId": observability.CorrelationID(r.Context()),
	})
}
