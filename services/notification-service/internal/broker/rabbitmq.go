// Package broker consume la cola de notificaciones en RabbitMQ y aplica la
// decisión del consumidor (ADR-005, 3.1 y 3.5):
//
//   - Ack: confirma el mensaje.
//   - Retry: lo publica en notifications.retry con un TTL igual a la espera y
//     el encabezado x-retry-count; al vencer, RabbitMQ lo devuelve a la cola
//     principal. Mientras tanto, los demás mensajes siguen (RN-34).
//   - DeadLetter: lo publica en notifications.dlq con el motivo.
//
// En los dos últimos casos el original se confirma recién cuando el broker
// confirmó la copia, así un mensaje nunca se pierde. La topología la crea
// rabbitmq-init: el usuario del servicio no puede declarar colas.
package broker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
)

// Colas del ADR-005 (3.1).
const (
	Queue      = "notifications.summary-moderation"
	RetryQueue = "notifications.retry"
	DLQ        = "notifications.dlq"
)

// Encabezados de los mensajes reintentados o fallidos.
const (
	HeaderRetryCount = "x-retry-count"
	HeaderReason     = "x-failure-reason"
)

// Prefetch es cuántos mensajes sin confirmar recibe a la vez (ADR-005, 3.1).
const Prefetch = 10

const publishTimeout = 5 * time.Second

// Handler procesa un mensaje y decide qué hacer con él.
type Handler interface {
	Handle(ctx context.Context, msg consumer.Message) consumer.Decision
}

// Consumer consume la cola y se reconecta con espera exponencial (1 s, 2 s,
// 4 s…, hasta 30 s) si RabbitMQ se cae o todavía no arrancó.
type Consumer struct {
	URL     string
	Handler Handler
	Logger  *slog.Logger
	// Las colas; vacías, las del ADR-005 (los tests usan otras).
	Queue, RetryQueue, DLQ string
}

func (c *Consumer) names() (string, string, string) {
	q, r, d := c.Queue, c.RetryQueue, c.DLQ
	if q == "" {
		q = Queue
	}
	if r == "" {
		r = RetryQueue
	}
	if d == "" {
		d = DLQ
	}
	return q, r, d
}

// Run consume hasta que se cancele ctx.
func (c *Consumer) Run(ctx context.Context) {
	wait := time.Second
	for {
		started := time.Now()
		err := c.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			wait = time.Second // estuvo andando: la caída es nueva
		}
		c.Logger.Warn("RabbitMQ no disponible, reintentando", "waitMs", wait.Milliseconds(), "error", errString(err))
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// consume abre la conexión y procesa mensajes hasta que se corte o se cancele ctx.
func (c *Consumer) consume(ctx context.Context) error {
	queue, retryQueue, dlq := c.names()
	conn, err := amqp.DialConfig(c.URL, amqp.Config{
		Dial:       amqp.DefaultDial(5 * time.Second),
		Properties: amqp.Table{"connection_name": "notification-service"},
	})
	if err != nil {
		return fmt.Errorf("conectar: %w", err)
	}
	defer conn.Close()

	in, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	if err := in.Qos(Prefetch, 0, false); err != nil {
		return fmt.Errorf("prefetch: %w", err)
	}
	// Canal aparte, en modo confirmación, para los reintentos y la DLQ.
	out, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abrir canal: %w", err)
	}
	if err := out.Confirm(false); err != nil {
		return fmt.Errorf("activar confirmaciones: %w", err)
	}
	deliveries, err := in.ConsumeWithContext(ctx, queue, "notification-service", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumir %s: %w", queue, err)
	}
	c.Logger.Info("consumiendo eventos de moderación", "queue", queue, "prefetch", Prefetch)

	// Un worker por mensaje sin confirmar: un mail lento (hasta 10 s) no frena a los demás.
	var wg sync.WaitGroup
	for range Prefetch {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range deliveries {
				c.process(ctx, out, d, retryQueue, dlq)
				if out.IsClosed() {
					// Sin canal para reintentos y DLQ no se puede seguir:
					// se corta la conexión y Run se reconecta.
					_ = conn.Close()
				}
			}
		}()
	}
	// deliveries se cierra al cancelar ctx o cuando se corta la conexión o el canal.
	closed := conn.NotifyClose(make(chan *amqp.Error, 1))
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case err := <-closed:
		if err != nil {
			return err
		}
	default:
	}
	return errors.New("se cortó el consumo de la cola")
}

func (c *Consumer) process(ctx context.Context, out *amqp.Channel, d amqp.Delivery, retryQueue, dlq string) {
	retries := retryCount(d.Headers)
	decision := c.Handler.Handle(ctx, consumer.Message{Body: d.Body, MessageID: d.MessageId, Retries: retries})

	var err error
	switch decision.Action {
	case consumer.Retry:
		err = c.forward(ctx, out, d, retryQueue, retries+1, decision)
	case consumer.DeadLetter:
		err = c.forward(ctx, out, d, dlq, retries, decision)
	}
	if err != nil {
		// No se pudo dejar la copia: el original vuelve a la cola y se procesa
		// de nuevo (el eventId evita el mail duplicado).
		c.Logger.Error("no se pudo reencolar el mensaje, vuelve a la cola", "messageId", d.MessageId, "error", err.Error())
		_ = d.Nack(false, true)
		return
	}
	if err := d.Ack(false); err != nil {
		c.Logger.Error("no se pudo confirmar el mensaje", "messageId", d.MessageId, "error", err.Error())
	}
}

// forward publica una copia del mensaje en otra cola, por el exchange por
// defecto, y espera la confirmación del broker.
func (c *Consumer) forward(ctx context.Context, out *amqp.Channel, d amqp.Delivery, queue string, retries int, decision consumer.Decision) error {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[HeaderRetryCount] = int32(retries)
	headers[HeaderReason] = decision.Reason
	msg := amqp.Publishing{
		Headers:       headers,
		ContentType:   d.ContentType,
		DeliveryMode:  amqp.Persistent,
		MessageId:     d.MessageId,
		CorrelationId: d.CorrelationId,
		Type:          d.Type,
		Timestamp:     d.Timestamp,
		AppId:         d.AppId,
		Body:          d.Body,
	}
	if decision.Action == consumer.Retry {
		// TTL por mensaje: cuando vence, notifications.retry lo devuelve a la principal.
		msg.Expiration = strconv.FormatInt(max(decision.Delay.Milliseconds(), 1), 10)
	}
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	confirm, err := out.PublishWithDeferredConfirmWithContext(pubCtx, "", queue, false, false, msg)
	if err != nil {
		return err
	}
	ok, err := confirm.WaitContext(pubCtx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("RabbitMQ rechazó el mensaje en %s", queue)
	}
	return nil
}

// retryCount lee x-retry-count, que puede llegar como distintos tipos enteros.
func retryCount(h amqp.Table) int {
	switch v := h[HeaderRetryCount].(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
