// Package club es el adaptador del Club de Beneficios de Metalfitness, la
// capacidad que publica el Grupo 1 (ADR-009). Es el único lugar del código que
// conoce su contrato (docs/contracts/consumidos/club-beneficios-v2.yaml): sus
// rutas, encabezados, campos y códigos de error. Hacia afuera expone tipos y
// errores propios, así el resto de users-service no depende del proveedor.
//
// Resiliencia (ADR-009, 3 y 4):
//   - cada intento tiene un timeout (AttemptTimeout) y toda la llamada un
//     presupuesto (CallBudget);
//   - se reintenta a lo sumo una vez, sólo ante errores transitorios (red,
//     timeout, 429, 502, 503 y 504), siempre con la misma Idempotency-Key;
//   - un circuit breaker deja de llamar al Club después de 3 fallas seguidas
//     durante 30 s, y después deja pasar un pedido de prueba.
package club

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Valores del ADR-009 (actualización del 2026-11-09). El Club responde en
// unos 2 s cuando su despliegue está frío y su gateway corta a los 5 s, por
// eso el intento tiene 4 s y no los 2 s del diseño preliminar.
const (
	AttemptTimeout  = 4 * time.Second
	CallBudget      = 6 * time.Second
	retryWait       = 200 * time.Millisecond
	breakerFailures = 3
	breakerOpenFor  = 30 * time.Second
)

// Kind clasifica los errores del Club para que el servicio decida qué hacer.
type Kind int

const (
	// Unavailable: no respondió, respondió 5xx o 429, o la respuesta no cumple
	// el contrato. Es transitorio: se puede reintentar más tarde.
	Unavailable Kind = iota + 1
	// NotLinked: la cuenta no está vinculada a Estudiario (404 ACCOUNT_NOT_FOUND).
	NotLinked
	// Rejected: el Club rechazó el pedido (400, 409). No se arregla reintentando.
	Rejected
	// Misconfigured: la API key falta, es inválida o el partner está suspendido (401, 403).
	Misconfigured
)

// Error es un error del Club traducido.
type Error struct {
	Kind          Kind
	Status        int    // estado HTTP, 0 si no hubo respuesta
	Code          string // code del Problem Details del Club
	CorrelationID string // el que devolvió el Club, para reportarle un problema
	Err           error
}

func (e *Error) Error() string {
	msg := "club de beneficios: " + kindName(e.Kind)
	if e.Status != 0 {
		msg += fmt.Sprintf(" (%d %s)", e.Status, e.Code)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

func kindName(k Kind) string {
	switch k {
	case Unavailable:
		return "no disponible"
	case NotLinked:
		return "cuenta no vinculada"
	case Rejected:
		return "pedido rechazado"
	case Misconfigured:
		return "mal configurado"
	}
	return "error"
}

// Is permite preguntar errors.Is(err, club.ErrUnavailable) y similares.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Kind == e.Kind && t.Status == 0 && t.Err == nil
}

// Errores para comparar con errors.Is.
var (
	ErrUnavailable   = &Error{Kind: Unavailable}
	ErrNotLinked     = &Error{Kind: NotLinked}
	ErrRejected      = &Error{Kind: Rejected}
	ErrMisconfigured = &Error{Kind: Misconfigured}
)

// Registration son los datos para dar de alta o vincular a una persona.
type Registration struct {
	Name           string
	Email          string
	ExternalUserID string
	EventID        string
}

// Receipt es el estado de un registro: "accepted" mientras el Club lo procesa
// o espera la confirmación por mail, "completed" cuando la cuenta quedó vinculada.
type Receipt struct {
	EventID        string
	ExternalUserID string
	Completed      bool
}

// Credit es una acreditación de puntos.
type Credit struct {
	EventID   string
	Points    int
	Reason    string
	Reference string
}

// Movement es un movimiento de la cuenta.
type Movement struct {
	ID         string
	Points     int
	Reason     string
	OccurredAt time.Time
}

// Balance es el saldo de la cuenta.
type Balance struct {
	Points    int
	UpdatedAt time.Time
}

// Client llama al Club por HTTP.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	breaker *gobreaker.CircuitBreaker[response]
	logger  *slog.Logger
	// Tiempos; New pone los del ADR-009 y los tests los acortan.
	attemptTimeout, callBudget, retryWait time.Duration
	// CorrelationID devuelve el correlationId del pedido en curso, para
	// mandarlo al Club en X-Correlation-Id.
	CorrelationID func(context.Context) string
}

// New arma el cliente. baseURL es la raíz del contrato, sin /v2 (por ejemplo
// https://metalfitness-club.vercel.app/partner-api o el mock).
func New(baseURL, apiKey string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		logger:  logger,

		attemptTimeout: AttemptTimeout,
		callBudget:     CallBudget,
		retryWait:      retryWait,
	}
	c.breaker = gobreaker.NewCircuitBreaker[response](gobreaker.Settings{
		Name:        "club-beneficios",
		MaxRequests: 1,
		Timeout:     breakerOpenFor,
		ReadyToTrip: func(n gobreaker.Counts) bool { return n.ConsecutiveFailures >= breakerFailures },
		// Cuentan como falla del Club sólo la indisponibilidad y la API key
		// rechazada; un 400 o un 404 es un problema del pedido, no del Club.
		IsSuccessful: func(err error) bool {
			return err == nil || errors.Is(err, ErrNotLinked) || errors.Is(err, ErrRejected)
		},
		IsExcluded: func(err error) bool { return errors.Is(err, context.Canceled) },
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("cambió el estado del circuit breaker",
				slog.String("breaker", name), slog.String("from", from.String()), slog.String("to", to.String()))
		},
	})
	return c
}

// uuidPattern es el formato que pide el contrato para X-Correlation-Id.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// BreakerState devuelve el estado del circuit breaker ("closed", "open" o "half-open").
func (c *Client) BreakerState() string { return c.breaker.State().String() }

// ---------- Operaciones ----------

// Register pide el alta o la vinculación (POST /v2/external-register). El
// Club responde 202 y termina después, cuando la persona confirma por mail.
func (c *Client) Register(ctx context.Context, r Registration) (Receipt, error) {
	body := registrationBody{Name: r.Name, Email: r.Email, ExternalUserID: r.ExternalUserID, EventID: r.EventID}
	var out receiptBody
	if err := c.call(ctx, http.MethodPost, "/v2/external-register", r.EventID, body, http.StatusAccepted, &out); err != nil {
		return Receipt{}, err
	}
	return out.toReceipt()
}

// Registration consulta el estado de un registro (GET /v2/external-registrations/{eventId}).
func (c *Client) Registration(ctx context.Context, eventID string) (Receipt, error) {
	var out receiptBody
	if err := c.call(ctx, http.MethodGet, "/v2/external-registrations/"+url.PathEscape(eventID), "", nil, http.StatusOK, &out); err != nil {
		return Receipt{}, err
	}
	return out.toReceipt()
}

// Credit acredita puntos (POST /v2/accounts/{externalUserId}/credits). La
// Idempotency-Key es el eventId: el mismo hecho nunca se acredita dos veces,
// aunque se reintente o el mensaje llegue repetido.
func (c *Client) Credit(ctx context.Context, externalUserID string, cr Credit) (Movement, error) {
	body := creditBody{EventID: cr.EventID, Points: cr.Points, Reason: cr.Reason, Reference: cr.Reference}
	var out operationBody
	path := "/v2/accounts/" + url.PathEscape(externalUserID) + "/credits"
	if err := c.call(ctx, http.MethodPost, path, cr.EventID, body, http.StatusCreated, &out); err != nil {
		return Movement{}, err
	}
	if out.MovementID == "" || out.OccurredAt.IsZero() {
		return Movement{}, invalid("la acreditación no trae movementId u occurredAt")
	}
	return Movement{ID: out.MovementID, Points: out.Points, Reason: out.Reason, OccurredAt: out.OccurredAt}, nil
}

// Balance consulta el saldo (GET /v2/accounts/{externalUserId}/balance).
func (c *Client) Balance(ctx context.Context, externalUserID string) (Balance, error) {
	var out balanceBody
	path := "/v2/accounts/" + url.PathEscape(externalUserID) + "/balance"
	if err := c.call(ctx, http.MethodGet, path, "", nil, http.StatusOK, &out); err != nil {
		return Balance{}, err
	}
	if out.Balance == nil || out.Balance.n < 0 {
		return Balance{}, invalid("el saldo falta o es negativo")
	}
	return Balance{Points: out.Balance.n, UpdatedAt: out.UpdatedAt}, nil
}

// Movements lista los movimientos que hizo Estudiario, los más nuevos primero
// (GET /v2/accounts/{externalUserId}/movements).
func (c *Client) Movements(ctx context.Context, externalUserID string, size int) ([]Movement, error) {
	var out movementPageBody
	path := "/v2/accounts/" + url.PathEscape(externalUserID) + "/movements?page=1&size=" + strconv.Itoa(size)
	if err := c.call(ctx, http.MethodGet, path, "", nil, http.StatusOK, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		return nil, invalid("la página de movimientos no trae items")
	}
	moves := make([]Movement, 0, len(out.Items))
	for _, m := range out.Items {
		moves = append(moves, Movement{ID: m.ID, Points: m.Points, Reason: m.Reason, OccurredAt: m.OccurredAt})
	}
	return moves, nil
}

// ---------- HTTP ----------

type response struct {
	status int
	body   []byte
	header http.Header
}

// call hace la llamada y la cuenta en users_club_requests_total por resultado.
func (c *Client) call(ctx context.Context, method, path, idemKey string, in any, want int, out any) error {
	err := c.callClub(ctx, method, path, idemKey, in, want, out)
	requests.WithLabelValues(resultOf(err)).Inc()
	return err
}

// callClub hace la llamada con el presupuesto, el reintento y el circuit
// breaker, y decodifica la respuesta esperada en out.
func (c *Client) callClub(ctx context.Context, method, path, idemKey string, in any, want int, out any) error {
	if c.apiKey == "" {
		return &Error{Kind: Misconfigured, Err: errors.New("falta CLUB_API_KEY")}
	}
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.callBudget)
	defer cancel()

	started := time.Now()
	var resp response
	var err error
	for attempt := 1; attempt <= 2; attempt++ {
		resp, err = c.breaker.Execute(func() (response, error) {
			r, err := c.do(ctx, method, path, idemKey, payload)
			if err != nil {
				return r, err
			}
			return r, classify(r, want)
		})
		if err == nil || attempt == 2 || !retryable(err, resp) {
			break
		}
		wait := c.retryWait + time.Duration(rand.Int64N(int64(c.retryWait/5)+1))
		if ra := retryAfter(resp.header); ra > 0 {
			wait = ra
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait+c.attemptTimeout/4 {
			break // no entra otro intento en el presupuesto
		}
		c.logger.WarnContext(ctx, "se reintenta la llamada al Club", "method", method, "path", logPath(path), "error", err.Error())
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return &Error{Kind: Unavailable, Err: ctx.Err()}
		}
	}

	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		err = &Error{Kind: Unavailable, Err: errors.New("circuit breaker abierto")}
	}
	if err != nil {
		level := slog.LevelWarn
		if errors.Is(err, ErrMisconfigured) || errors.Is(err, ErrRejected) {
			level = slog.LevelError // no se arregla reintentando: hay que revisar el pedido o la clave
		}
		c.logger.Log(ctx, level, "falló la llamada al Club", "method", method, "path", logPath(path),
			"durationMs", time.Since(started).Milliseconds(), "status", resp.status,
			"clubCorrelationId", resp.header.Get("X-Correlation-Id"), "error", err.Error())
		return err
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return invalid("la respuesta no es JSON válido: " + err.Error())
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path, idemKey string, payload []byte) (response, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(attemptCtx, method, c.baseURL+path, body)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	// El contrato del Club pide un UUID en X-Correlation-Id. Nuestros
	// correlationId no siempre lo son (el gateway genera otro formato): en ese
	// caso no se manda, el Club genera uno y lo devuelve, y queda en el log como
	// clubCorrelationId junto con el nuestro.
	if c.CorrelationID != nil {
		if id := c.CorrelationID(ctx); uuidPattern.MatchString(id) {
			req.Header.Set("X-Correlation-Id", id)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return response{}, ctx.Err()
		}
		return response{}, &Error{Kind: Unavailable, Err: err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return response{}, &Error{Kind: Unavailable, Err: err}
	}
	return response{status: res.StatusCode, body: data, header: res.Header}, nil
}

// classify traduce el estado HTTP según la tabla del ADR-009 (4) y los
// códigos del contrato del Club.
func classify(r response, want int) error {
	if r.status == want {
		return nil
	}
	var p struct {
		Code          string `json:"code"`
		CorrelationID string `json:"correlationId"`
		Detail        string `json:"detail"`
	}
	_ = json.Unmarshal(r.body, &p)
	e := &Error{Status: r.status, Code: p.Code, CorrelationID: p.CorrelationID}
	if p.Detail != "" {
		e.Err = errors.New(p.Detail)
	}
	switch {
	case r.status == http.StatusNotFound:
		e.Kind = NotLinked
	case r.status == http.StatusUnauthorized || r.status == http.StatusForbidden:
		e.Kind = Misconfigured
	case r.status == http.StatusConflict && p.Code == "IDEMPOTENCY_IN_PROGRESS":
		e.Kind = Unavailable // la primera todavía se procesa: se reintenta con la misma clave
	case r.status == http.StatusTooManyRequests || r.status >= 500:
		e.Kind = Unavailable
	case r.status >= 400:
		e.Kind = Rejected
	default:
		e.Kind = Unavailable // 2xx o 3xx inesperado: no cumple el contrato
		e.Err = fmt.Errorf("se esperaba %d", want)
	}
	return e
}

// retryable dice si conviene un segundo intento: sólo ante indisponibilidad,
// y no con 500 (puede no ser transitorio, ADR-009) ni con el circuito abierto.
func retryable(err error, r response) bool {
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return false
	}
	var e *Error
	if !errors.As(err, &e) || e.Kind != Unavailable {
		return false
	}
	return r.status != http.StatusInternalServerError
}

func retryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	s, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

func invalid(msg string) error {
	return &Error{Kind: Unavailable, Err: errors.New("respuesta inválida: " + msg)}
}

// logPath saca de la ruta el id externo del usuario, para no llenar los logs de datos personales.
func logPath(p string) string {
	if strings.HasPrefix(p, "/v2/accounts/") {
		rest := strings.TrimPrefix(p, "/v2/accounts/")
		if i := strings.Index(rest, "/"); i >= 0 {
			return "/v2/accounts/{externalUserId}" + rest[i:]
		}
	}
	return p
}

// ---------- Cuerpos del contrato v2 ----------

type registrationBody struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	ExternalUserID string `json:"externalUserId"`
	EventID        string `json:"eventId"`
}

type receiptBody struct {
	EventID        string `json:"eventId"`
	ExternalUserID string `json:"externalUserId"`
	Status         string `json:"status"`
}

// toReceipt valida lo que se usa. Un estado desconocido se trata como
// "todavía no terminó" (lector tolerante, ADR-009).
func (b receiptBody) toReceipt() (Receipt, error) {
	if b.EventID == "" || b.ExternalUserID == "" || b.Status == "" {
		return Receipt{}, invalid("el registro no trae eventId, externalUserId o status")
	}
	return Receipt{EventID: b.EventID, ExternalUserID: b.ExternalUserID, Completed: b.Status == "completed"}, nil
}

type creditBody struct {
	EventID   string `json:"eventId"`
	Points    int    `json:"points"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}

type operationBody struct {
	MovementID string    `json:"movementId"`
	Points     int       `json:"points"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurredAt"`
}

// number distingue un saldo 0 de un saldo que no vino.
type number struct{ n int }

func (n *number) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &n.n) }

type balanceBody struct {
	Balance   *number   `json:"balance"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type movementPageBody struct {
	Items []struct {
		ID         string    `json:"id"`
		Points     int       `json:"points"`
		Reason     string    `json:"reason"`
		OccurredAt time.Time `json:"occurredAt"`
	} `json:"items"`
}
