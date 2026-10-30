// Package correlation guarda el identificador de correlación del pedido en el
// context.Context, para que los logs y los eventos del outbox lo incluyan
// (ADR-005, 2.5). Lo carga el middleware HTTP a partir de X-Correlation-Id.
package correlation

import "context"

// Header es el encabezado HTTP que transporta el identificador de correlación.
const Header = "X-Correlation-Id"

type key struct{}

// WithID devuelve un contexto que lleva el identificador de correlación.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// ID devuelve el identificador de correlación del contexto, o "" si no tiene.
func ID(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}
