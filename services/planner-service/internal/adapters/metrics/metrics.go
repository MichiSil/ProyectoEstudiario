// Package metrics es el adaptador de Prometheus del puerto app.Metrics: cuenta
// las propuestas por quién las armó y las confirmaciones por resultado
// (ADR-011). Las expone GET /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

var (
	proposals = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "planner_proposals_total",
		Help: "Propuestas de plan generadas, por quién las armó: ia o reglas (RN-09).",
	}, []string{"generated_by"})
	confirmations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "planner_confirmations_total",
		Help: "Confirmaciones de plan por resultado: confirmada, rechazada o error (RN-10 a RN-14).",
	}, []string{"result"})
)

// Cada serie existe desde el arranque en 0: así increase() también cuenta la
// primera propuesta o confirmación.
func init() {
	for _, by := range []domain.GeneratedBy{domain.GeneratedByAI, domain.GeneratedByRules} {
		proposals.WithLabelValues(string(by))
	}
	for _, r := range []string{app.ConfirmOK, app.ConfirmRejected, app.ConfirmFailed} {
		confirmations.WithLabelValues(r)
	}
}

// Prometheus implementa app.Metrics.
type Prometheus struct{}

func (Prometheus) ProposalGenerated(by domain.GeneratedBy) {
	proposals.WithLabelValues(string(by)).Inc()
}

func (Prometheus) PlanConfirmed(result string) {
	confirmations.WithLabelValues(result).Inc()
}
