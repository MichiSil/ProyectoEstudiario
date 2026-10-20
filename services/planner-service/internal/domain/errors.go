package domain

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound          = errors.New("recurso no encontrado")
	ErrPlanNotProposed   = errors.New("el plan no está en estado propuesto")
	ErrAlreadyConfirmed  = errors.New("el examen ya tiene un plan confirmado vigente")
	ErrConfirmInProgress = errors.New("hay una confirmación en curso con la misma clave de idempotencia")
	ErrIdempotencyReused = errors.New("la clave de idempotencia ya se usó para otro plan")
	// ErrUnavailable indica que una dependencia (por ejemplo PostgreSQL) no
	// está disponible. No es un error del pedido: se puede reintentar más tarde.
	ErrUnavailable = errors.New("servicio no disponible temporalmente")
)

// RuleViolation describe una regla de negocio incumplida, con el identificador
// de la regla del SPEC (por ejemplo "RN-12") para que el error sea rastreable.
type RuleViolation struct {
	Rule         string `json:"rule"`
	Message      string `json:"message"`
	SessionIndex int    `json:"sessionIndex"` // -1 si no corresponde a una sesión puntual
}

// ValidationError agrupa todas las reglas incumplidas de una operación.
type ValidationError struct {
	Violations []RuleViolation
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		parts[i] = fmt.Sprintf("%s: %s", v.Rule, v.Message)
	}
	return "reglas incumplidas: " + strings.Join(parts, "; ")
}

func (e *ValidationError) add(rule string, index int, format string, args ...any) {
	e.Violations = append(e.Violations, RuleViolation{Rule: rule, Message: fmt.Sprintf(format, args...), SessionIndex: index})
}

func (e *ValidationError) orNil() error {
	if len(e.Violations) == 0 {
		return nil
	}
	return e
}
