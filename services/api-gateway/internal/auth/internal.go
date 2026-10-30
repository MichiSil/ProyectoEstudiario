package auth

import (
	"errors"
	"net/http"
)

// InternalTokenHeader lleva el secreto compartido entre el gateway y los
// servicios internos. Con él, planner-service sabe que la identidad de
// X-User-Id la puso el gateway y no alguien que lo llamó directo a su puerto
// (que queda publicado para la capacidad de otros grupos, ADR-008).
const InternalTokenHeader = "X-Internal-Token"

// ErrWeakInternalToken indica que INTERNAL_TOKEN falta o es demasiado corto.
var ErrWeakInternalToken = errors.New("INTERNAL_TOKEN falta o tiene menos de 32 caracteres")

// WithInternalToken agrega el secreto interno a los pedidos que siguen hacia un
// servicio, reemplazando cualquier valor que haya mandado el cliente.
func WithInternalToken(token string, next http.Handler) (http.Handler, error) {
	if len(token) < MinSecretLen {
		return nil, ErrWeakInternalToken
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())
		r.Header.Set(InternalTokenHeader, token)
		next.ServeHTTP(w, r)
	}), nil
}
