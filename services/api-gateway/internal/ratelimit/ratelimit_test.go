package ratelimit

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestLimiter(rate Rate) (*Limiter, *clock) {
	c := &clock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	l := New(rate)
	l.now = c.now
	return l, c
}

func TestRafagaYDespuesEspera(t *testing.T) {
	l, c := newTestLimiter(Rate{PerMinute: 60, Burst: 3})
	for i := range 3 {
		if ok, _, _ := l.Allow("ana"); !ok {
			t.Fatalf("el pedido %d de la ráfaga no pasó", i+1)
		}
	}
	ok, remaining, wait := l.Allow("ana")
	if ok || remaining != 0 || wait <= 0 || wait > time.Second {
		t.Fatalf("cuarto pedido: ok = %v, remaining = %d, wait = %v", ok, remaining, wait)
	}
	c.t = c.t.Add(time.Second) // 60 por minuto: una ficha por segundo
	if ok, _, _ := l.Allow("ana"); !ok {
		t.Fatal("después de 1 s tenía que pasar")
	}
}

func TestCadaClaveTieneSuCupo(t *testing.T) {
	l, _ := newTestLimiter(Rate{PerMinute: 60, Burst: 1})
	if ok, _, _ := l.Allow("ana"); !ok {
		t.Fatal("ana")
	}
	if ok, _, _ := l.Allow("beto"); !ok {
		t.Fatal("el cupo de ana no tiene que frenar a beto")
	}
}

func TestOlvidaLasClavesInactivas(t *testing.T) {
	l, c := newTestLimiter(Rate{PerMinute: 60, Burst: 1})
	l.Allow("ana")
	c.t = c.t.Add(idleAfter + 2*time.Minute)
	l.Allow("beto")
	if _, ok := l.buckets["ana"]; ok {
		t.Fatal("no se olvidó la clave inactiva")
	}
}

func TestMiddlewareResponde429ConRetryAfter(t *testing.T) {
	l, _ := newTestLimiter(Rate{PerMinute: 60, Burst: 2})
	calls := 0
	h := l.Middleware("usuario", ByHeader("X-User-Id"), slog.New(slog.NewTextHandler(io.Discard, nil)),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/planner/subjects", nil)
		req.Header.Set("X-User-Id", "ana")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	do()
	if rec := do(); rec.Code != http.StatusOK || rec.Header().Get("RateLimit-Remaining") != "0" || rec.Header().Get("RateLimit-Limit") != "60" {
		t.Fatalf("segundo pedido: %d %v", rec.Code, rec.Header())
	}
	rec := do()
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusTooManyRequests || body["code"] != "RATE_LIMITED" || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("tercer pedido: %d %v %v", rec.Code, body, rec.Header())
	}
	if calls != 2 {
		t.Fatalf("el servicio recibió %d pedidos: el rechazado no tiene que llegar", calls)
	}
}

func TestIPDelCliente(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/users/login", nil)
	req.RemoteAddr = "172.18.0.5:41000"
	if got := ByClientIP(req); got != "172.18.0.5" {
		t.Errorf("sin X-Forwarded-For: %q", got)
	}
	req.Header.Set("X-Forwarded-For", "190.2.3.4, 172.18.0.9")
	if got := ByClientIP(req); got != "190.2.3.4" {
		t.Errorf("con X-Forwarded-For: %q", got)
	}
}

func TestCupoDesdeVariable(t *testing.T) {
	if r := FromEnv("600", DefaultUser); r.PerMinute != 600 || r.Burst != 300 {
		t.Errorf("600 → %+v", r)
	}
	for _, v := range []string{"", "x", "0", "-5"} {
		if r := FromEnv(v, DefaultPublic); r != DefaultPublic {
			t.Errorf("%q → %+v, quería el valor por defecto", v, r)
		}
	}
}
