package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/adapters/rules"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/correlation"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

var cba, _ = time.LoadLocation("America/Argentina/Cordoba")

// Lunes 19/10/2026 a las 10:00.
var now = time.Date(2026, 10, 19, 10, 0, 0, 0, cba)

// input: materia difícil, parcial dentro de 14 días y disponibilidad de 18 a
// 21 todos los días, con sesiones de 90 minutos.
func input() app.ProposalInput {
	var slots []domain.Slot
	for d := time.Sunday; d <= time.Saturday; d++ {
		slots = append(slots, domain.Slot{Weekday: d, StartMinute: 18 * 60, EndMinute: 21 * 60})
	}
	return app.ProposalInput{
		Subject: domain.Subject{ID: "mat-1", Name: "Análisis II", Difficulty: domain.DifficultyHard},
		Exam: domain.Exam{ID: "ex-1", Type: domain.ExamParcial, StartsAt: now.AddDate(0, 0, 14),
			Topics: []string{"Integrales dobles", "Integrales triples"}, Status: domain.ExamScheduled},
		Availability: domain.Availability{Slots: slots, MaxDailyMinutes: 120, PreferredSessionMinutes: 90, Location: cba},
		Now:          now,
	}
}

const validPlan = `{"sessions": [
  {"date": "2026-10-20", "startTime": "18:00", "durationMinutes": 90, "topic": "Integrales dobles", "objective": "Leer teoría de integrales dobles", "reasoning": "Es la base del segundo tema."},
  {"date": "2026-10-22", "startTime": "18:00", "durationMinutes": 90, "topic": "Integrales triples", "objective": "Leer teoría de integrales triples", "reasoning": "Extiende el tema anterior."},
  {"date": "2026-11-01", "startTime": "18:00", "durationMinutes": 90, "topic": "Repaso general", "objective": "Simulacro de parcial", "reasoning": "Último día antes del parcial."}
]}`

// chatReply arma una respuesta de chat completions con el contenido dado.
func chatReply(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
	})
	return string(b)
}

// fakeGroq levanta un servidor HTTP falso que responde con handler y cuenta
// los pedidos recibidos.
func fakeGroq(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{APIKey: "clave-de-prueba", Model: "modelo-de-prueba", BaseURL: srv.URL, Timeout: 8 * time.Second}), &hits
}

func reply(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, chatReply(content))
	}
}

func TestClient_RespuestaValida(t *testing.T) {
	var got chatRequest
	var auth, corr string
	client, _ := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("ruta = %q, se esperaba /chat/completions", r.URL.Path)
		}
		auth, corr = r.Header.Get("Authorization"), r.Header.Get(correlation.Header)
		json.NewDecoder(r.Body).Decode(&got)
		reply(validPlan)(w, r)
	})

	ctx := correlation.WithID(context.Background(), "corr-123")
	p, err := client.Generate(ctx, input())
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if p.GeneratedBy != domain.GeneratedByAI || len(p.Warnings) != 0 {
		t.Fatalf("se esperaba una propuesta de la IA sin avisos, llegó %q %v", p.GeneratedBy, p.Warnings)
	}
	if len(p.Drafts) != 3 {
		t.Fatalf("se esperaban 3 sesiones, llegaron %d", len(p.Drafts))
	}
	first := p.Drafts[0]
	if !first.Start.Equal(time.Date(2026, 10, 20, 18, 0, 0, 0, cba)) || first.DurationMinutes != 90 ||
		first.Topic != "Integrales dobles" || first.Reasoning == "" {
		t.Errorf("primera sesión inesperada: %+v", first)
	}

	if auth != "Bearer clave-de-prueba" || corr != "corr-123" {
		t.Errorf("encabezados: Authorization=%q, %s=%q", auth, correlation.Header, corr)
	}
	if got.Model != "modelo-de-prueba" || got.ResponseFormat["type"] != "json_object" || len(got.Messages) != 2 {
		t.Errorf("pedido inesperado: %+v", got)
	}
	prompt := got.Messages[1].Content
	for _, want := range []string{"Integrales dobles", "Integrales triples", "difícil", "lunes de 18:00 a 21:00", "2026-10-20 (martes): 18:00 a 21:00", "90 minutos", "2026-11-02 10:00"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("el prompt no menciona %q:\n%s", want, prompt)
		}
	}
}

// El modelo a veces envuelve el JSON en un bloque de código aunque se le pida
// sólo JSON.
func TestClient_AceptaBloqueDeCodigo(t *testing.T) {
	client, _ := fakeGroq(t, reply("```json\n"+validPlan+"\n```"))
	if _, err := client.Generate(context.Background(), input()); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
}

func TestClient_Errores(t *testing.T) {
	outsideSlot := strings.Replace(validPlan, `"startTime": "18:00"`, `"startTime": "20:00"`, 1) // termina 21:30
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"JSON inválido", reply("acá va tu plan: lunes teoría, martes práctica"), ErrInvalidResponse},
		{"sin sesiones", reply(`{"sessions": []}`), ErrInvalidResponse},
		{"sesión fuera de franja", reply(outsideSlot), ErrInvalidResponse},
		{"duración distinta", reply(strings.ReplaceAll(validPlan, `"durationMinutes": 90`, `"durationMinutes": 60`)), ErrInvalidResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := fakeGroq(t, tc.handler)
			if _, err := client.Generate(context.Background(), input()); !errors.Is(err, tc.want) {
				t.Fatalf("se esperaba %v, llegó %v", tc.want, err)
			}
		})
	}
}

// Ante un 5xx se reintenta una sola vez (ADR-005).
func TestClient_Reintenta5xxUnaVez(t *testing.T) {
	client, hits := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"overloaded"}`, http.StatusServiceUnavailable)
	})
	_, err := client.Generate(context.Background(), input())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusServiceUnavailable {
		t.Fatalf("se esperaba StatusError 503, llegó %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("se esperaban 2 intentos, hubo %d", hits.Load())
	}
}

func TestClient_ReintentoQueFunciona(t *testing.T) {
	var calls atomic.Int32
	client, _ := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "", http.StatusBadGateway)
			return
		}
		reply(validPlan)(w, r)
	})
	if _, err := client.Generate(context.Background(), input()); err != nil {
		t.Fatalf("el reintento debería haber funcionado: %v", err)
	}
}

// Un 4xx (por ejemplo, clave inválida) no se reintenta.
func TestClient_NoReintenta4xx(t *testing.T) {
	client, hits := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
	})
	if _, err := client.Generate(context.Background(), input()); err == nil {
		t.Fatal("se esperaba error")
	}
	if hits.Load() != 1 {
		t.Fatalf("se esperaba 1 intento, hubo %d", hits.Load())
	}
}

// --- Decorador con fallback (RN-09, CA-05) ---

// slowHandler no responde hasta que el cliente corta la llamada o termina el
// test (en Windows el servidor no siempre se entera de que el cliente cortó, y
// srv.Close esperaría al handler).
func slowHandler(t *testing.T) http.HandlerFunc {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
}

func fixed(h http.HandlerFunc) func(*testing.T) http.HandlerFunc {
	return func(*testing.T) http.HandlerFunc { return h }
}

func TestFallback(t *testing.T) {
	cases := []struct {
		name       string
		handler    func(t *testing.T) http.HandlerFunc
		timeout    time.Duration
		wantBy     domain.GeneratedBy
		wantReason string
	}{
		{"respuesta válida", fixed(reply(validPlan)), 8 * time.Second, domain.GeneratedByAI, ""},
		{"timeout", slowHandler, 200 * time.Millisecond, domain.GeneratedByRules, "timeout"},
		{"JSON inválido", fixed(reply("{esto no es json")), 8 * time.Second, domain.GeneratedByRules, "invalid_response"},
		{"error 5xx", fixed(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "", http.StatusInternalServerError)
		}), 8 * time.Second, domain.GeneratedByRules, "http_status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// El handler se crea después del servidor para que su Cleanup
			// corra antes que srv.Close.
			var handler http.HandlerFunc
			client, _ := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) { handler(w, r) })
			handler = tc.handler(t)
			client.cfg.Timeout = tc.timeout
			var logs bytes.Buffer
			gen := WithFallback(client, rules.Engine{}, slog.New(slog.NewJSONHandler(&logs, nil)))

			ctx := correlation.WithID(context.Background(), "corr-"+tc.name)
			started := time.Now()
			p, err := gen.Generate(ctx, input())
			if err != nil {
				t.Fatalf("no se esperaba error: %v", err)
			}
			if elapsed := time.Since(started); elapsed > tc.timeout+time.Second {
				t.Errorf("tardó %s, más que el presupuesto de %s", elapsed, tc.timeout)
			}
			if p.GeneratedBy != tc.wantBy {
				t.Fatalf("GeneratedBy = %q, se esperaba %q", p.GeneratedBy, tc.wantBy)
			}
			// La propuesta siempre se puede confirmar, venga de donde venga.
			in := input()
			if err := domain.ValidatePlan(p.Drafts, in.Exam, in.Availability, in.Booked, in.Now); err != nil {
				t.Fatalf("la propuesta no cumple las reglas: %v", err)
			}

			if tc.wantBy == domain.GeneratedByAI {
				if len(p.Warnings) != 0 || logs.Len() != 0 {
					t.Fatalf("sin falla no debería haber avisos ni logs: %v %s", p.Warnings, logs.String())
				}
				return
			}
			if len(p.Warnings) != 1 || p.Warnings[0] != app.WarningAIFallback {
				t.Fatalf("se esperaba el aviso %s, llegó %v", app.WarningAIFallback, p.Warnings)
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("el log no es JSON: %v\n%s", err, logs.String())
			}
			if entry["correlationId"] != "corr-"+tc.name || entry["reason"] != tc.wantReason || entry["level"] != "WARN" {
				t.Fatalf("log inesperado: %s", logs.String())
			}
		})
	}
}

// Si la IA y el motor de reglas fallan, se devuelve el error del motor.
func TestFallback_ErrorDelMotorDeReglas(t *testing.T) {
	client, _ := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "", http.StatusInternalServerError) })
	gen := WithFallback(client, rules.Engine{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	in := input()
	in.Availability.Slots = nil
	if _, err := gen.Generate(context.Background(), in); !errors.Is(err, rules.ErrNoAvailability) {
		t.Fatalf("se esperaba ErrNoAvailability, llegó %v", err)
	}
}

// Después de 3 fallas seguidas el circuito se abre y la IA no se llama más:
// la propuesta sale directo del motor de reglas, sin esperar.
func TestFallback_CircuitBreaker(t *testing.T) {
	client, hits := fakeGroq(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "", http.StatusInternalServerError) })
	var logs bytes.Buffer
	gen := WithFallback(client, rules.Engine{}, slog.New(slog.NewJSONHandler(&logs, nil)))

	for i := 0; i < breakerFailures; i++ {
		gen.Generate(context.Background(), input())
	}
	before := hits.Load()
	p, err := gen.Generate(context.Background(), input())
	if err != nil || p.GeneratedBy != domain.GeneratedByRules {
		t.Fatalf("se esperaba la propuesta por reglas, llegó %q %v", p.GeneratedBy, err)
	}
	if hits.Load() != before {
		t.Fatalf("con el circuito abierto no debería llamarse a la IA (%d → %d pedidos)", before, hits.Load())
	}
	if !strings.Contains(logs.String(), `"reason":"circuit_open"`) {
		t.Fatalf("se esperaba un log con reason=circuit_open:\n%s", logs.String())
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("AI_API_KEY", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_BASE_URL", "")
	t.Setenv("AI_TIMEOUT", "")
	if _, err := ConfigFromEnv(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("se esperaba ErrMissingAPIKey, llegó %v", err)
	}

	t.Setenv("AI_API_KEY", "gsk_prueba")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.BaseURL != DefaultBaseURL || cfg.Model != DefaultModel || cfg.Timeout != DefaultTimeout {
		t.Fatalf("valores por defecto inesperados: %+v %v", cfg, err)
	}

	t.Setenv("AI_TIMEOUT", "5s")
	if cfg, _ := ConfigFromEnv(); cfg.Timeout != 5*time.Second {
		t.Fatalf("AI_TIMEOUT no se aplicó: %s", cfg.Timeout)
	}
	t.Setenv("AI_TIMEOUT", "ocho")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("se esperaba error por AI_TIMEOUT inválido")
	}
}
