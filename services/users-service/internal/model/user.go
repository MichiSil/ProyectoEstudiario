// Package model tiene los tipos y errores que comparten las capas de
// users-service (controller → service → repository, ADR-002).
package model

import (
	"errors"
	"time"
)

// Roles del SPEC (RF-03). Son los mismos que valida el api-gateway en el JWT.
const (
	RoleStudent = "estudiante"
	RoleAdmin   = "admin"
)

// User es un usuario registrado. PasswordHash nunca sale del servicio.
type User struct {
	ID           string
	Name         string
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

var (
	// ErrEmailInUse: ya hay un usuario con ese mail (RN-01).
	ErrEmailInUse = errors.New("ya existe un usuario con ese mail")
	// ErrInvalidCredentials: el mail no existe o la contraseña no coincide.
	// Es el mismo error en los dos casos para no revelar qué mails están registrados.
	ErrInvalidCredentials = errors.New("mail o contraseña incorrectos")
	// ErrNotFound: el usuario no existe.
	ErrNotFound = errors.New("el usuario no existe")
	// ErrUnavailable: la base no respondió a tiempo o no está disponible (ADR-010).
	ErrUnavailable = errors.New("la base de datos no está disponible")
)

// FieldError es un problema de validación en un campo del pedido.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError agrupa los problemas de validación de un pedido.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	return "el pedido tiene datos inválidos"
}
