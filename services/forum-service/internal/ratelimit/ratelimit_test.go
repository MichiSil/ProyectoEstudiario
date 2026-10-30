package ratelimit

import (
	"testing"
	"time"
)

func TestCupoDe60PedidosY10Registros(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	l := New()
	l.now = func() time.Time { return now }

	for i := 0; i < RegistersPerMinute; i++ {
		if !l.Allow("grupo4", true).Allowed {
			t.Fatalf("el registro %d se rechazó antes del cupo", i+1)
		}
	}
	if d := l.Allow("grupo4", true); d.Allowed || d.RetryAfter != time.Minute {
		t.Fatalf("registro 11: %+v; se esperaba rechazo con Retry-After de 60 s", d)
	}
	for i := RegistersPerMinute; i < RequestsPerMinute; i++ {
		if !l.Allow("grupo4", false).Allowed {
			t.Fatalf("el pedido %d se rechazó antes del cupo", i+1)
		}
	}
	d := l.Allow("grupo4", false)
	if d.Allowed || d.Remaining != 0 || d.Limit != 60 {
		t.Fatalf("pedido 61: %+v; se esperaba rechazo sin cupo restante", d)
	}
	if !l.Allow("grupo7", true).Allowed {
		t.Error("el cupo de una API key afectó a otra")
	}

	now = now.Add(time.Minute)
	if d := l.Allow("grupo4", true); !d.Allowed || d.Remaining != 59 {
		t.Errorf("al minuto siguiente: %+v; se esperaba el cupo renovado", d)
	}
}
