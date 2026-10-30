package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Exchange es el exchange de eventos de forum-service (ADR-005, 3.1). Lo crea
// rabbitmq-init: el usuario de forum-service sólo puede publicar en él.
const Exchange = "forum.events"

// publishTimeout es lo máximo que se espera la confirmación del broker.
const publishTimeout = 5 * time.Second

// RabbitMQ publica con confirmación del broker (publisher confirms). Abre la
// conexión la primera vez que publica y, si algo falla, la descarta para
// reconectarse en el próximo intento del relay.
type RabbitMQ struct {
	URL      string
	Exchange string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// Publish publica el evento como mensaje persistente, con la routing key igual
// al tipo de evento y el eventId como message_id, y espera el ack del broker.
func (r *RabbitMQ) Publish(ctx context.Context, ev model.OutboxEvent) error {
	body, err := json.Marshal(Envelope(ev))
	if err != nil {
		return fmt.Errorf("evento %s: %w", ev.EventID, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	ch, err := r.channel()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	exchange := r.Exchange
	if exchange == "" {
		exchange = Exchange
	}
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, ev.EventType, false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     ev.EventID,
		CorrelationId: ev.CorrelationID,
		Type:          ev.EventType,
		Timestamp:     ev.OccurredAt,
		AppId:         ev.Producer,
		Body:          body,
	})
	if err != nil {
		r.reset()
		return fmt.Errorf("publicar en RabbitMQ: %w", err)
	}
	ok, err := confirm.WaitContext(ctx)
	if err != nil {
		r.reset()
		return fmt.Errorf("esperar la confirmación de RabbitMQ: %w", err)
	}
	if !ok {
		return errors.New("RabbitMQ rechazó el evento (nack)")
	}
	return nil
}

// channel devuelve el canal abierto o abre uno nuevo en modo confirmación.
func (r *RabbitMQ) channel() (*amqp.Channel, error) {
	if r.ch != nil && !r.ch.IsClosed() {
		return r.ch, nil
	}
	r.reset()
	conn, err := amqp.DialConfig(r.URL, amqp.Config{Dial: amqp.DefaultDial(5 * time.Second), Properties: amqp.Table{"connection_name": "forum-service relay"}})
	if err != nil {
		return nil, fmt.Errorf("conectar a RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("abrir canal en RabbitMQ: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("activar las confirmaciones de RabbitMQ: %w", err)
	}
	r.conn, r.ch = conn, ch
	return ch, nil
}

func (r *RabbitMQ) reset() {
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.conn, r.ch = nil, nil
}

// Close cierra la conexión.
func (r *RabbitMQ) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reset()
}

// envelope es el sobre común de los eventos (ADR-005, 3.2).
type envelope struct {
	EventID       string         `json:"eventId"`
	EventType     string         `json:"eventType"`
	EventVersion  int            `json:"eventVersion"`
	OccurredAt    time.Time      `json:"occurredAt"`
	Producer      string         `json:"producer"`
	CorrelationID string         `json:"correlationId,omitempty"`
	Data          map[string]any `json:"data"`
}

// Envelope arma el JSON del evento. Los datos vienen de MongoDB, donde los
// subdocumentos y las fechas se leen como tipos de BSON: se pasan a tipos de
// Go para que el JSON quede como en el ADR-005.
func Envelope(ev model.OutboxEvent) any {
	data, _ := plain(ev.Data).(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	return envelope{
		EventID:       ev.EventID,
		EventType:     ev.EventType,
		EventVersion:  ev.EventVersion,
		OccurredAt:    ev.OccurredAt.UTC(),
		Producer:      ev.Producer,
		CorrelationID: ev.CorrelationID,
		Data:          data,
	}
}

func plain(v any) any {
	switch x := v.(type) {
	case bson.D:
		m := make(map[string]any, len(x))
		for _, e := range x {
			m[e.Key] = plain(e.Value)
		}
		return m
	case bson.M:
		return plain(map[string]any(x))
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = plain(e)
		}
		return m
	case bson.A:
		return plain([]any(x))
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case bson.DateTime:
		return x.Time().UTC()
	case time.Time:
		return x.UTC()
	default:
		return v
	}
}
