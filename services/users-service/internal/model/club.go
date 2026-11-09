package model

import "time"

// Estados de la vinculación con el Club de Beneficios (RF-28, ADR-009).
const (
	ClubLinkPending = "pendiente" // el Club recibió el pedido; falta que la persona confirme por mail
	ClubLinkActive  = "vinculada"
)

// Estados de una acreditación de puntos.
const (
	CreditPending  = "pendiente"  // falta acreditarla: sin vincular o el Club no respondió (RN-43)
	CreditDone     = "acreditado" // el Club la registró
	CreditRejected = "rechazado"  // el Club la rechazó y reintentar no la arregla
)

// ClubLink es la vinculación de un usuario con su cuenta del Club.
type ClubLink struct {
	UserID         string
	ExternalUserID string
	EventID        string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ClubCredit son puntos a acreditar en el Club por un hecho de Estudiario,
// por ejemplo un resumen aprobado. EventID identifica el hecho (RN-42).
type ClubCredit struct {
	EventID    string
	UserID     string
	Reference  string
	Points     int
	Reason     string
	Status     string
	MovementID string
	CreatedAt  time.Time
	CreditedAt *time.Time
}
