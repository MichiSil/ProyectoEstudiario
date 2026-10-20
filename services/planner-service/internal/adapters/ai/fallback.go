package ai

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/correlation"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// Valores del circuit breaker (ADR-010): se abre después de 3 fallas seguidas
// de la IA, queda abierto 30 s (en ese tiempo se usa directo el motor de
// reglas, sin esperar a la IA) y después deja pasar un pedido de prueba.
const (
	breakerFailures = 3
	breakerOpenFor  = 30 * time.Second
)

// Fallback es un PlanGenerator que primero intenta con la IA y, si falla,
// tarda más que su presupuesto o devuelve una propuesta inválida, arma la
// propuesta con el motor de reglas (RN-09).
type Fallback struct {
	primary  app.PlanGenerator
	fallback app.PlanGenerator
	breaker  *gobreaker.CircuitBreaker[app.Proposal]
	log      *slog.Logger
}

var _ app.PlanGenerator = (*Fallback)(nil)

// WithFallback combina el agente de IA (primary) con el motor de reglas
// (fallback). Si logger es nil se usa slog.Default().
func WithFallback(primary, fallback app.PlanGenerator, logger *slog.Logger) *Fallback {
	if logger == nil {
		logger = slog.Default()
	}
	f := &Fallback{primary: primary, fallback: fallback, log: logger}
	f.breaker = gobreaker.NewCircuitBreaker[app.Proposal](gobreaker.Settings{
		Name:        "ia",
		MaxRequests: 1,
		Timeout:     breakerOpenFor,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= breakerFailures },
		// Si quien pidió el plan canceló el pedido, no es culpa de la IA.
		IsExcluded: func(err error) bool { return errors.Is(err, context.Canceled) },
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("cambió el estado del circuit breaker",
				slog.String("breaker", name), slog.String("from", from.String()), slog.String("to", to.String()))
		},
	})
	return f
}

func (f *Fallback) Generate(ctx context.Context, in app.ProposalInput) (app.Proposal, error) {
	started := time.Now()
	p, err := f.breaker.Execute(func() (app.Proposal, error) {
		return f.primary.Generate(ctx, in)
	})
	if err == nil {
		return p, nil
	}

	f.log.LogAttrs(ctx, slog.LevelWarn, "la IA no pudo armar la propuesta; se usa el motor de reglas",
		slog.String("correlationId", correlation.ID(ctx)),
		slog.String("examId", in.Exam.ID),
		slog.String("reason", failureReason(err)),
		slog.String("error", err.Error()),
		slog.String("breakerState", f.breaker.State().String()),
		slog.Int64("elapsedMs", time.Since(started).Milliseconds()),
	)

	p, err = f.fallback.Generate(ctx, in)
	if err != nil {
		return app.Proposal{}, err
	}
	p.GeneratedBy = domain.GeneratedByRules
	p.Warnings = append(p.Warnings, app.WarningAIFallback)
	return p, nil
}

// failureReason clasifica la falla para poder filtrar los logs y las métricas.
func failureReason(err error) string {
	var se *StatusError
	switch {
	case errors.Is(err, gobreaker.ErrOpenState), errors.Is(err, gobreaker.ErrTooManyRequests):
		return "circuit_open"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrInvalidResponse):
		return "invalid_response"
	case errors.As(err, &se):
		return "http_status"
	default:
		return "unavailable"
	}
}
