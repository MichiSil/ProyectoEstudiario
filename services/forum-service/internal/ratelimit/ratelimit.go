// Package ratelimit aplica el cupo de uso de la capacidad publicada (ADR-014):
// por API key, 60 pedidos por minuto, de los cuales hasta 10 registros.
//
// Cuenta en memoria y por ventanas fijas de un minuto. Si forum-service corre
// en varias instancias, cada una cuenta por su lado y el cupo efectivo se
// multiplica por la cantidad de instancias; se acepta, como en el ADR-008.
package ratelimit

import (
	"sync"
	"time"
)

// Cupos por minuto (ADR-014).
const (
	RequestsPerMinute  = 60
	RegistersPerMinute = 10
)

// Decision es el resultado de pedir permiso para un pedido.
type Decision struct {
	Allowed    bool
	Limit      int           // RateLimit-Limit
	Remaining  int           // RateLimit-Remaining
	RetryAfter time.Duration // cuánto falta para que empiece el próximo minuto
}

type window struct {
	start     time.Time
	requests  int
	registers int
}

// Limiter lleva la cuenta por consumidor.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
	now     func() time.Time
}

func New() *Limiter {
	return &Limiter{windows: map[string]*window{}, now: time.Now}
}

// Allow cuenta un pedido del consumidor. register indica si es un registro
// (POST /v1/documents), que además consume el cupo de registros.
func (l *Limiter) Allow(consumer string, register bool) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.windows[consumer]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &window{start: now}
		l.windows[consumer] = w
	}
	retry := w.start.Add(time.Minute).Sub(now)
	d := Decision{Limit: RequestsPerMinute, RetryAfter: retry}
	if w.requests >= RequestsPerMinute || (register && w.registers >= RegistersPerMinute) {
		d.Remaining = RequestsPerMinute - w.requests
		return d
	}
	w.requests++
	if register {
		w.registers++
	}
	d.Allowed = true
	d.Remaining = RequestsPerMinute - w.requests
	return d
}
