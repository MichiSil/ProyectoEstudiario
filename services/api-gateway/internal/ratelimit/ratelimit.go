// Package ratelimit es el control de tráfico del api-gateway (enunciado:
// "punto de entrada único con autenticación, direccionamiento y control de
// tráfico"). Limita los pedidos con un balde de fichas (token bucket) por
// clave:
//
//   - en las rutas con JWT, la clave es el usuario (X-User-Id que puso el
//     gateway después de validar el token): un estudiante no puede acaparar
//     el sistema, y se cuenta igual aunque use varios dispositivos;
//   - en las rutas públicas (registrarse e iniciar sesión), la clave es la IP
//     del cliente: frena los intentos de adivinar contraseñas.
//
// Al superar el límite responde 429 RATE_LIMITED con Retry-After, sin llegar
// al servicio. Las respuestas informan el cupo en RateLimit-Limit y
// RateLimit-Remaining. El estado vive en memoria del gateway, que corre en
// una sola instancia.
package ratelimit

import (
	"encoding/json"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/observability"
)

// Rate es un cupo: PerMinute pedidos por minuto de forma sostenida, con
// ráfagas de hasta Burst pedidos seguidos.
type Rate struct {
	PerMinute int
	Burst     int
}

// Valores por defecto (configurables con RATE_LIMIT_USER_PER_MIN y
// RATE_LIMIT_PUBLIC_PER_MIN). Una pantalla del frontend hace entre 3 y 10
// pedidos al abrirse, así que 120 por minuto con ráfagas de 60 no se nota en
// el uso normal.
var (
	DefaultUser   = Rate{PerMinute: 120, Burst: 60}
	DefaultPublic = Rate{PerMinute: 20, Burst: 10}
)

// idleAfter es cuánto tiempo sin pedidos tiene que pasar para olvidar una
// clave (su balde ya estaría lleno de nuevo).
const idleAfter = 10 * time.Minute

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter guarda un balde por clave.
type Limiter struct {
	rate Rate
	now  func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

// New arma un limitador con ese cupo.
func New(rate Rate) *Limiter {
	if rate.Burst < 1 {
		rate.Burst = 1
	}
	return &Limiter{rate: rate, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow descuenta una ficha de la clave. Devuelve si el pedido pasa, cuántas
// fichas quedan y, si no pasa, cuánto falta para la próxima.
func (l *Limiter) Allow(key string) (ok bool, remaining int, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)

	perSecond := float64(l.rate.PerMinute) / 60
	b, found := l.buckets[key]
	if !found {
		b = &bucket{tokens: float64(l.rate.Burst), last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(float64(l.rate.Burst), b.tokens+now.Sub(b.last).Seconds()*perSecond)
	b.last = now
	if b.tokens < 1 {
		wait := time.Duration((1 - b.tokens) / perSecond * float64(time.Second))
		return false, 0, wait
	}
	b.tokens--
	return true, int(b.tokens), 0
}

// sweep olvida las claves inactivas, así el mapa no crece sin límite.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < time.Minute {
		return
	}
	l.swept = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > idleAfter {
			delete(l.buckets, k)
		}
	}
}

// Middleware aplica el límite a next con la clave que devuelve keyOf.
func (l *Limiter) Middleware(scope string, keyOf func(*http.Request) string, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, remaining, wait := l.Allow(scope + ":" + keyOf(r))
		w.Header().Set("RateLimit-Limit", strconv.Itoa(l.rate.PerMinute))
		w.Header().Set("RateLimit-Remaining", strconv.Itoa(remaining))
		if !ok {
			seconds := int(math.Ceil(wait.Seconds()))
			logger.WarnContext(r.Context(), "pedido rechazado por el límite de tráfico", "scope", scope, "retryAfterSeconds", seconds)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":          "about:blank",
				"title":         "demasiados pedidos, probá de nuevo en unos segundos",
				"status":        http.StatusTooManyRequests,
				"code":          "RATE_LIMITED",
				"correlationId": observability.CorrelationID(r.Context()),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ByHeader usa como clave un encabezado (por ejemplo X-User-Id, que el gateway
// completa después de validar el JWT, así el cliente no lo puede elegir).
func ByHeader(name string) func(*http.Request) string {
	return func(r *http.Request) string { return r.Header.Get(name) }
}

// ByClientIP usa como clave la IP del cliente. Detrás del frontend (nginx) y,
// en la nube, de Caddy, la IP real es la primera de X-Forwarded-For: Caddy
// descarta la que mande el cliente y pone la suya. Sin ese encabezado, se usa
// la dirección de la conexión.
func ByClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// FromEnv lee un cupo por minuto de una variable; la ráfaga es la mitad. Si
// la variable falta o es inválida, devuelve def.
func FromEnv(value string, def Rate) Rate {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return def
	}
	return Rate{PerMinute: n, Burst: max(1, n/2)}
}
