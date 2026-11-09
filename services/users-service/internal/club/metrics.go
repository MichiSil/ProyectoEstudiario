package club

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sony/gobreaker/v2"
)

// Resultados de una llamada al Club en las métricas (ADR-011, ADR-015).
const (
	ResultOK            = "ok"
	ResultUnavailable   = "indisponible"
	ResultNotLinked     = "no_vinculada"
	ResultRejected      = "rechazado"
	ResultMisconfigured = "mal_configurado"
)

var requests = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "users_club_requests_total",
	Help: "Llamadas al Club de Beneficios del Grupo 1 por resultado: ok, indisponible, no_vinculada, rechazado o mal_configurado (ADR-015).",
}, []string{"result"})

// Cada serie existe desde el arranque en 0: así increase() también cuenta la
// primera llamada de cada resultado.
func init() {
	for _, r := range []string{ResultOK, ResultUnavailable, ResultNotLinked, ResultRejected, ResultMisconfigured} {
		requests.WithLabelValues(r)
	}
}

// resultOf clasifica el resultado de una llamada. Un circuito abierto cuenta
// como indisponible, igual que para el servicio.
func resultOf(err error) string {
	switch {
	case err == nil:
		return ResultOK
	case errors.Is(err, ErrNotLinked):
		return ResultNotLinked
	case errors.Is(err, ErrRejected):
		return ResultRejected
	case errors.Is(err, ErrMisconfigured):
		return ResultMisconfigured
	}
	return ResultUnavailable
}

// BreakerGauge es la métrica users_club_breaker_state del circuit breaker de
// este cliente: 0 cerrado, 1 semiabierto (probando) y 2 abierto. Se registra
// una vez, en el arranque.
func (c *Client) BreakerGauge() prometheus.GaugeFunc {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "users_club_breaker_state",
		Help: "Estado del circuit breaker del Club de Beneficios: 0 cerrado, 1 semiabierto, 2 abierto (ADR-015).",
	}, func() float64 {
		switch c.breaker.State() {
		case gobreaker.StateHalfOpen:
			return 1
		case gobreaker.StateOpen:
			return 2
		}
		return 0
	})
}
