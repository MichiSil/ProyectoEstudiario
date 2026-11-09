package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

var _ app.Metrics = Prometheus{}

func TestCuentaPropuestasYConfirmaciones(t *testing.T) {
	ia := testutil.ToFloat64(proposals.WithLabelValues("ia"))
	ok := testutil.ToFloat64(confirmations.WithLabelValues(app.ConfirmOK))
	Prometheus{}.ProposalGenerated(domain.GeneratedByAI)
	Prometheus{}.PlanConfirmed(app.ConfirmOK)
	if got := testutil.ToFloat64(proposals.WithLabelValues("ia")) - ia; got != 1 {
		t.Errorf("propuestas de ia = %v, se esperaba 1", got)
	}
	if got := testutil.ToFloat64(confirmations.WithLabelValues(app.ConfirmOK)) - ok; got != 1 {
		t.Errorf("confirmadas = %v, se esperaba 1", got)
	}
}
