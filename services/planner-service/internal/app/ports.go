// Package app contiene los casos de uso de planner-service y los puertos
// (interfaces) que implementan los adaptadores. Es el centro de la
// arquitectura hexagonal: depende sólo del dominio.
package app

import (
	"context"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// ProposalInput es todo lo que necesita un generador para proponer un plan.
type ProposalInput struct {
	Subject      domain.Subject
	Exam         domain.Exam
	Availability domain.Availability
	// Now es el momento en que se pide la propuesta. Ninguna sesión puede
	// empezar antes.
	Now time.Time
	// Materials es el material que entra en el examen (teóricos y prácticos
	// reales del estudiante). Vacío si no cargó ninguno.
	Materials []domain.Material
	// Booked son las sesiones ya reservadas del estudiante. La propuesta no
	// puede superponerse con ellas ni superar con ellas la capacidad diaria.
	Booked []domain.Session
}

// Proposal es el resultado de un generador: las sesiones propuestas, quién
// las armó y los avisos para el usuario (por ejemplo "AI_FALLBACK" cuando la
// IA no respondió y se usó el motor de reglas, RN-09).
type Proposal struct {
	Drafts      []domain.SessionDraft
	GeneratedBy domain.GeneratedBy
	Warnings    []string
}

// WarningAIFallback indica que la propuesta la armó el motor de reglas porque
// el agente de IA no estaba disponible (RN-09).
const WarningAIFallback = "AI_FALLBACK"

// WarningInsufficientTime indica que entre hoy y el examen no hay horarios
// libres para todas las sesiones que harían falta: el plan junta temas en
// menos sesiones. El estudiante puede sumar franjas o subir la capacidad diaria.
const WarningInsufficientTime = "INSUFFICIENT_TIME"

// PlanGenerator propone las sesiones de un plan. Lo implementan el motor de
// reglas (adapters/rules) y el agente de IA (adapters/ai).
type PlanGenerator interface {
	Generate(ctx context.Context, in ProposalInput) (Proposal, error)
}

// ConfirmResult es la respuesta de una confirmación. Se guarda junto con la
// clave de idempotencia para devolver lo mismo ante un reintento (RN-14).
type ConfirmResult struct {
	PlanID   string           `json:"planId"`
	Sessions []domain.Session `json:"sessions"`
}

// PlanRepository es el puerto de persistencia de planner-service.
type PlanRepository interface {
	CreateSubject(ctx context.Context, s domain.Subject) error
	GetSubject(ctx context.Context, studentID, subjectID string) (domain.Subject, error)
	ListSubjects(ctx context.Context, studentID string) ([]domain.Subject, error)

	CreateExam(ctx context.Context, e domain.Exam) error
	GetExam(ctx context.Context, studentID, examID string) (domain.Exam, error)
	// ListExams devuelve los exámenes del estudiante, con el estado de su plan vigente.
	ListExams(ctx context.Context, studentID string) ([]domain.Exam, error)
	// UpdateExam cambia un examen dentro de una transacción con las filas del
	// estudiante bloqueadas. apply recibe el examen actual y devuelve el nuevo
	// y si el plan confirmado pasa a desactualizado (RN-16).
	UpdateExam(ctx context.Context, studentID, examID string, apply func(domain.Exam) (domain.Exam, bool, error)) (domain.Exam, error)
	// CancelExam cancela el examen, cancela sus planes y libera sus sesiones
	// no completadas, todo en una transacción (RN-17). Cancelar un examen ya
	// cancelado no cambia nada.
	CancelExam(ctx context.Context, studentID, examID string) (domain.Exam, error)

	CreateMaterial(ctx context.Context, m domain.Material) error
	ListMaterials(ctx context.Context, studentID, subjectID string) ([]domain.Material, error)
	DeleteMaterial(ctx context.Context, studentID, materialID string) error

	SaveAvailability(ctx context.Context, a domain.Availability) error
	GetAvailability(ctx context.Context, studentID string) (domain.Availability, error)

	ListBookedSessions(ctx context.Context, studentID string, from time.Time) ([]domain.Session, error)

	SaveProposal(ctx context.Context, p domain.Plan) error
	GetPlan(ctx context.Context, studentID, planID string) (domain.Plan, error)

	// Confirm reserva las sesiones del plan dentro de una única transacción.
	// validate se ejecuta dentro de la transacción, con las sesiones del
	// estudiante ya bloqueadas, para que dos confirmaciones concurrentes no
	// pasen las validaciones a la vez (RN-12, RN-13, RN-14). Si el examen
	// tenía un plan desactualizado, sus sesiones no completadas se liberan
	// antes de validar: el plan nuevo las reemplaza (RN-16).
	Confirm(ctx context.Context, req ConfirmRequest, validate func(booked []domain.Session) error) (ConfirmResult, error)
}

type ConfirmRequest struct {
	StudentID      string
	PlanID         string
	IdempotencyKey string
}

// Clock permite fijar la hora en los tests.
type Clock interface{ Now() time.Time }

// IDGenerator genera identificadores únicos.
type IDGenerator interface{ NewID() string }
