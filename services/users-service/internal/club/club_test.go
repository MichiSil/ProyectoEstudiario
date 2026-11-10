package club

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Tests del adaptador con un Club falso (ADR-009, 6): respuesta válida,
// errores de cada tipo, timeout, reintento, respuesta inválida y circuit breaker.

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "clave-de-prueba", slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.attemptTimeout = 300 * time.Millisecond
	c.callBudget = 2 * time.Second
	c.retryWait = 10 * time.Millisecond
	return c, srv
}

func problem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": "x", "status": status, "code": code, "correlationId": "club-123",
	})
}

func TestAcreditarMandaClaveEventoYEncabezados(t *testing.T) {
	var got struct {
		path, key, idem, ctype string
		body                   map[string]any
	}
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got.path, got.key, got.idem, got.ctype = r.URL.Path, r.Header.Get("X-API-Key"), r.Header.Get("Idempotency-Key"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"movementId":"m-1","externalUserId":"estudiario-u1","type":"CREDIT","points":50,
			"reason":"Resumen aprobado en Estudiario","reference":"s1","occurredAt":"2026-11-09T18:00:00Z","balance":50,"campoNuevo":true}`)
	})
	mv, err := c.Credit(context.Background(), "estudiario-u1", Credit{EventID: "resumen-aprobado-s1", Points: 50, Reason: "Resumen aprobado en Estudiario", Reference: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if mv.ID != "m-1" || mv.Points != 50 {
		t.Errorf("movimiento = %+v", mv)
	}
	if got.path != "/v2/accounts/estudiario-u1/credits" || got.key != "clave-de-prueba" || got.idem != "resumen-aprobado-s1" || got.ctype != "application/json" {
		t.Errorf("pedido = %+v", got)
	}
	if got.body["eventId"] != "resumen-aprobado-s1" || got.body["points"] != float64(50) {
		t.Errorf("cuerpo = %v", got.body)
	}
}

func TestErroresSeTraducen(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{404, "ACCOUNT_NOT_FOUND", ErrNotLinked},
		{401, "UNAUTHORIZED", ErrMisconfigured},
		{403, "FORBIDDEN", ErrMisconfigured},
		{400, "INVALID_REQUEST", ErrRejected},
		{409, "IDEMPOTENCY_CONFLICT", ErrRejected},
		{503, "UNAVAILABLE", ErrUnavailable},
		{500, "INTERNAL", ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { problem(w, tc.status, tc.code) })
			_, err := c.Balance(context.Background(), "estudiario-u1")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, quería %v", err, tc.want)
			}
			var e *Error
			if errors.As(err, &e) && e.CorrelationID != "club-123" {
				t.Errorf("no guardó el correlationId del Club: %+v", e)
			}
		})
	}
}

func TestReintentaUnaVezAnte503(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			problem(w, 503, "UNAVAILABLE")
			return
		}
		_, _ = io.WriteString(w, `{"externalUserId":"estudiario-u1","balance":120,"updatedAt":"2026-11-09T18:00:00Z"}`)
	})
	b, err := c.Balance(context.Background(), "estudiario-u1")
	if err != nil || b.Points != 120 || calls.Load() != 2 {
		t.Fatalf("saldo = %+v, err = %v, llamadas = %d", b, err, calls.Load())
	}
}

func TestNoReintentaAnte500NiAnte400(t *testing.T) {
	for _, status := range []int{500, 400} {
		var calls atomic.Int32
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			problem(w, status, "X")
		})
		_, _ = c.Balance(context.Background(), "estudiario-u1")
		if calls.Load() != 1 {
			t.Errorf("con %d hizo %d llamadas, quería 1", status, calls.Load())
		}
	}
}

func TestTimeoutEsIndisponibilidad(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	})
	started := time.Now()
	_, err := c.Balance(context.Background(), "estudiario-u1")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if time.Since(started) > 1500*time.Millisecond {
		t.Errorf("tardó %v: no respetó el timeout por intento", time.Since(started))
	}
}

func TestRespuestaQueNoCumpleElContratoEsIndisponibilidad(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"externalUserId":"estudiario-u1"}`) // falta balance
	})
	if _, err := c.Balance(context.Background(), "estudiario-u1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestCircuitBreakerSeAbreYNoLlama(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		problem(w, 500, "INTERNAL")
	})
	for range breakerFailures {
		_, _ = c.Balance(context.Background(), "estudiario-u1")
	}
	if c.BreakerState() != "open" {
		t.Fatalf("estado = %s, quería open", c.BreakerState())
	}
	before := calls.Load()
	started := time.Now()
	_, err := c.Balance(context.Background(), "estudiario-u1")
	if !errors.Is(err, ErrUnavailable) || calls.Load() != before {
		t.Fatalf("con el circuito abierto llamó igual (err = %v)", err)
	}
	if time.Since(started) > 50*time.Millisecond {
		t.Errorf("la respuesta degradada tardó %v", time.Since(started))
	}
}

func TestLos404NoAbrenElCircuito(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { problem(w, 404, "ACCOUNT_NOT_FOUND") })
	for range breakerFailures + 2 {
		_, _ = c.Balance(context.Background(), "estudiario-u1")
	}
	if c.BreakerState() != "closed" {
		t.Fatalf("estado = %s: un 404 es un problema del pedido, no del Club", c.BreakerState())
	}
}

func TestSinAPIKeyNoLlama(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	c.apiKey = ""
	if _, err := c.Balance(context.Background(), "x"); !errors.Is(err, ErrMisconfigured) || calls.Load() != 0 {
		t.Fatalf("err = %v, llamadas = %d", err, calls.Load())
	}
}

func TestRegistroPendienteYCompleto(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"registrationId":"38ef723a-0a63-44a8-a828-9d7c491c61fb","eventId":"vinculo-u1","externalUserId":"estudiario-u1","status":"accepted"}`)
			return
		}
		_, _ = io.WriteString(w, `{"registrationId":"38ef723a-0a63-44a8-a828-9d7c491c61fb","eventId":"vinculo-u1","externalUserId":"estudiario-u1","status":"completed"}`)
	})
	r, err := c.Register(context.Background(), Registration{Name: "Ana", Email: "ana@x.com", ExternalUserID: "estudiario-u1", EventID: "vinculo-u1"})
	if err != nil || r.Completed {
		t.Fatalf("registro = %+v, err = %v", r, err)
	}
	r, err = c.Registration(context.Background(), "vinculo-u1")
	if err != nil || !r.Completed {
		t.Fatalf("consulta = %+v, err = %v", r, err)
	}
}

func TestCorrelationIdSoloSiEsUUID(t *testing.T) {
	var got []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Correlation-Id"))
		_, _ = io.WriteString(w, `{"externalUserId":"estudiario-u1","balance":10,"updatedAt":"2026-11-09T18:00:00Z"}`)
	})
	for _, id := range []string{"herapbdhhae5pg3bjt5f2uf4hj", "3c5e7a9b-1d2f-4a6b-8c0d-2e4f6a8b0c1d"} {
		c.CorrelationID = func(context.Context) string { return id }
		if _, err := c.Balance(context.Background(), "estudiario-u1"); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0] != "" || got[1] != "3c5e7a9b-1d2f-4a6b-8c0d-2e4f6a8b0c1d" {
		t.Errorf("X-Correlation-Id mandados = %q; se esperaba sólo el UUID", got)
	}
}
