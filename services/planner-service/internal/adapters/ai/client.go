// Package ai implementa el agente de IA que propone el plan de estudio (RF-08)
// usando la API de Groq compatible con OpenAI (chat completions). La respuesta
// del modelo se pide en JSON y se valida con las mismas reglas que la
// confirmación del plan: si no las cumple, se descarta.
//
// El paquete también tiene el decorador WithFallback, que usa el motor de
// reglas cuando la IA falla, tarda o responde algo inválido (RN-09).
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/correlation"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// ErrInvalidResponse indica que la IA respondió, pero su propuesta no se puede
// usar: no es el JSON pedido o no cumple las reglas del plan.
var ErrInvalidResponse = errors.New("respuesta inválida de la IA")

// StatusError es una respuesta HTTP de error del proveedor de IA.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("el proveedor de IA respondió %d: %s", e.Code, e.Body)
}

// maxResponseBytes limita lo que se lee de la respuesta del proveedor.
const maxResponseBytes = 1 << 20

// Client es el agente de IA. Implementa app.PlanGenerator.
type Client struct {
	cfg  Config
	http *http.Client
}

var _ app.PlanGenerator = (*Client)(nil)

// NewClient crea el cliente. El timeout de cada llamada lo maneja Generate
// con el contexto, por eso el http.Client no tiene uno propio.
func NewClient(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{}}
}

// Generate pide la propuesta a la IA dentro del presupuesto total (8 s por
// defecto, ADR-005): un intento de hasta 3/4 del presupuesto y, como mucho,
// un reintento si el primero falló rápido por un error transitorio y queda
// al menos 3/8 del presupuesto. Con 8 s: intento de 6 s y reintento sólo si
// quedan 3 s o más.
func (c *Client) Generate(ctx context.Context, in app.ProposalInput) (app.Proposal, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	body, err := c.requestBody(in)
	if err != nil {
		return app.Proposal{}, err
	}

	content, err := c.attempt(ctx, body)
	if err != nil && retryable(err) && remaining(ctx) >= c.cfg.Timeout*3/8 {
		// Espera de 300 ms ± 20 % antes del único reintento.
		wait := time.Duration(float64(300*time.Millisecond) * (0.8 + 0.4*rand.Float64()))
		select {
		case <-time.After(wait):
			content, err = c.attempt(ctx, body)
		case <-ctx.Done():
			return app.Proposal{}, fmt.Errorf("llamada a la IA: %w", ctx.Err())
		}
	}
	if err != nil {
		return app.Proposal{}, err
	}

	drafts, err := parseDrafts(content, in)
	if err != nil {
		return app.Proposal{}, err
	}
	return app.Proposal{Drafts: drafts, GeneratedBy: domain.GeneratedByAI}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (c *Client) requestBody(in app.ProposalInput) ([]byte, error) {
	return json.Marshal(chatRequest{
		Model: c.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(in)},
		},
		Temperature:    0.2,
		ResponseFormat: map[string]string{"type": "json_object"},
	})
}

// attempt hace una llamada al proveedor y devuelve el contenido del mensaje.
func (c *Client) attempt(ctx context.Context, body []byte) (string, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout*3/4)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if id := correlation.ID(ctx); id != "" {
		req.Header.Set(correlation.Header, id)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llamada a la IA: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("leer la respuesta de la IA: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", &StatusError{Code: res.StatusCode, Body: truncate(string(raw), 300)}
	}

	var chat chatResponse
	if err := json.Unmarshal(raw, &chat); err != nil {
		return "", fmt.Errorf("%w: la respuesta del proveedor no es JSON: %v", ErrInvalidResponse, err)
	}
	if len(chat.Choices) == 0 || strings.TrimSpace(chat.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("%w: la respuesta no trae contenido", ErrInvalidResponse)
	}
	return chat.Choices[0].Message.Content, nil
}

// retryable indica si vale la pena reintentar: errores de red, 429 o 5xx.
// Un timeout no se reintenta, porque ya consumió casi todo el presupuesto.
func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	if errors.Is(err, ErrInvalidResponse) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

func remaining(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	return time.Until(deadline)
}

// aiPlan es el JSON que se le pide al modelo.
type aiPlan struct {
	Sessions []aiSession `json:"sessions"`
}

type aiSession struct {
	Date            string `json:"date"`
	StartTime       string `json:"startTime"`
	DurationMinutes int    `json:"durationMinutes"`
	Topic           string `json:"topic"`
	Objective       string `json:"objective"`
	Reasoning       string `json:"reasoning"`
}

// parseDrafts convierte el JSON del modelo en sesiones propuestas y verifica
// que la propuesta se pueda confirmar tal cual (RN-10 a RN-13), que respete
// la ventana de RN-06 y que todas las sesiones tengan la duración pedida.
func parseDrafts(content string, in app.ProposalInput) ([]domain.SessionDraft, error) {
	var plan aiPlan
	if err := json.Unmarshal([]byte(stripCodeFence(content)), &plan); err != nil {
		return nil, fmt.Errorf("%w: el contenido no es el JSON pedido: %v", ErrInvalidResponse, err)
	}
	if len(plan.Sessions) == 0 {
		return nil, fmt.Errorf("%w: la propuesta no tiene sesiones", ErrInvalidResponse)
	}

	loc := location(in)
	duration := sessionMinutes(in.Availability)
	from := windowStart(in)
	drafts := make([]domain.SessionDraft, 0, len(plan.Sessions))
	for i, s := range plan.Sessions {
		start, err := time.ParseInLocation("2006-01-02 15:04", s.Date+" "+s.StartTime, loc)
		if err != nil {
			return nil, fmt.Errorf("%w: la sesión %d tiene fecha u hora inválida (%q %q)", ErrInvalidResponse, i, s.Date, s.StartTime)
		}
		if strings.TrimSpace(s.Topic) == "" || strings.TrimSpace(s.Objective) == "" {
			return nil, fmt.Errorf("%w: a la sesión %d le falta el tema o el objetivo", ErrInvalidResponse, i)
		}
		if s.DurationMinutes != duration {
			return nil, fmt.Errorf("%w: la sesión %d dura %d minutos y tiene que durar %d", ErrInvalidResponse, i, s.DurationMinutes, duration)
		}
		if start.Before(from) {
			return nil, fmt.Errorf("%w: la sesión %d empieza antes de la ventana de estudio (RN-06)", ErrInvalidResponse, i)
		}
		drafts = append(drafts, domain.SessionDraft{
			Start:           start,
			DurationMinutes: s.DurationMinutes,
			Topic:           strings.TrimSpace(s.Topic),
			Objective:       strings.TrimSpace(s.Objective),
			Reasoning:       strings.TrimSpace(s.Reasoning),
		})
	}
	sort.SliceStable(drafts, func(a, b int) bool { return drafts[a].Start.Before(drafts[b].Start) })

	if err := domain.ValidatePlan(drafts, in.Exam, in.Availability, in.Booked, in.Now); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return drafts, nil
}

// stripCodeFence quita un bloque ```json ... ``` si el modelo lo agregó.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	return strings.TrimSpace(strings.TrimSuffix(s, "```"))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
