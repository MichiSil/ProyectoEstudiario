package club

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Cada llamada se cuenta por resultado y el breaker se ve en la métrica
// (ADR-011, ADR-015).
func TestMetricasDelClub(t *testing.T) {
	status := http.StatusNotFound
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		problem(w, status, "ACCOUNT_NOT_FOUND")
	})
	gauge := c.BreakerGauge()
	count := func(result string) float64 { return testutil.ToFloat64(requests.WithLabelValues(result)) }

	notLinked, unavailable := count(ResultNotLinked), count(ResultUnavailable)
	_, _ = c.Balance(context.Background(), "estudiario-u1")
	if got := count(ResultNotLinked) - notLinked; got != 1 {
		t.Errorf("no_vinculada = %v, se esperaba 1", got)
	}
	if got := testutil.ToFloat64(gauge); got != 0 {
		t.Errorf("breaker con un 404 = %v, se esperaba 0 (cerrado)", got)
	}

	// Con el Club caído, el breaker se abre y la siguiente cuenta como indisponible sin llamar.
	status = http.StatusInternalServerError
	for range breakerFailures + 1 {
		_, _ = c.Balance(context.Background(), "estudiario-u1")
	}
	if got := count(ResultUnavailable) - unavailable; got != float64(breakerFailures+1) {
		t.Errorf("indisponible = %v, se esperaban %d", got, breakerFailures+1)
	}
	if got := testutil.ToFloat64(gauge); got != 2 {
		t.Errorf("breaker = %v, se esperaba 2 (abierto)", got)
	}
}
