package broker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
)

// Resultados de un mensaje en las métricas (ADR-005, ADR-011).
const (
	ResultProcessed = "procesado"  // se mandó el mail o se descartó (duplicado, sin mail que mandar)
	ResultRetry     = "reintento"  // a notifications.retry
	ResultDLQ       = "dlq"        // a notifications.dlq
	ResultRequeued  = "reencolado" // no se pudo dejar la copia: vuelve a la cola principal
)

var messages = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "notification_messages_total",
	Help: "Mensajes de moderación consumidos, por resultado: procesado, reintento, dlq o reencolado (ADR-005).",
}, []string{"result"})

// Cada serie existe desde el arranque en 0: así increase() también cuenta el
// primer mensaje que va a la DLQ (y la alerta MensajesEnLaDLQ lo ve).
func init() {
	for _, r := range []string{ResultProcessed, ResultRetry, ResultDLQ, ResultRequeued} {
		messages.WithLabelValues(r)
	}
}

// resultOf dice cómo terminó un mensaje según la decisión del consumidor y si
// se pudo reencolar.
func resultOf(action consumer.Action, forwardErr error) string {
	switch {
	case forwardErr != nil:
		return ResultRequeued
	case action == consumer.Retry:
		return ResultRetry
	case action == consumer.DeadLetter:
		return ResultDLQ
	}
	return ResultProcessed
}
