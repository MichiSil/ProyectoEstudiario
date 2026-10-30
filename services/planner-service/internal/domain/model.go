// Package domain contiene las entidades y las reglas de negocio de la
// planificación del estudio (sección 6 del SPEC). No depende de ningún
// adaptador: ni HTTP, ni base de datos, ni proveedor de IA.
package domain

import "time"

// Difficulty es la dificultad que el estudiante le asigna a una materia:
// 1 fácil, 2 media, 3 difícil, 4 muy difícil.
type Difficulty int

const (
	DifficultyEasy     Difficulty = 1
	DifficultyMedium   Difficulty = 2
	DifficultyHard     Difficulty = 3
	DifficultyVeryHard Difficulty = 4
)

func (d Difficulty) Valid() bool { return d >= DifficultyEasy && d <= DifficultyVeryHard }

// AnticipationDays son los días de anticipación con los que se empieza a
// estudiar un examen según la dificultad de la materia (RN-06).
var AnticipationDays = map[Difficulty]int{
	DifficultyEasy:     5,
	DifficultyMedium:   10,
	DifficultyHard:     15,
	DifficultyVeryHard: 21,
}

type Subject struct {
	ID         string
	StudentID  string
	Name       string
	Professors []string
	Difficulty Difficulty
}

type ExamType string

const (
	ExamParcial       ExamType = "parcial"
	ExamFinal         ExamType = "final"
	ExamRecuperatorio ExamType = "recuperatorio"
	ExamEntrega       ExamType = "entrega"
)

func (t ExamType) Valid() bool {
	switch t {
	case ExamParcial, ExamFinal, ExamRecuperatorio, ExamEntrega:
		return true
	}
	return false
}

// ExamModality dice cómo se evalúa el examen. Vacía si el estudiante no la
// indicó: en ese caso el plan no asume si hay que leer teoría o hacer ejercicios.
type ExamModality string

const (
	ModalityTheory   ExamModality = "teorico"
	ModalityPractice ExamModality = "practico"
	ModalityMixed    ExamModality = "mixto"
)

func (m ExamModality) Valid() bool {
	switch m {
	case "", ModalityTheory, ModalityPractice, ModalityMixed:
		return true
	}
	return false
}

// MaterialKind distingue el material teórico (apuntes, unidades, capítulos)
// del práctico (trabajos prácticos, guías de ejercicios).
type MaterialKind string

const (
	MaterialTheory   MaterialKind = "teorico"
	MaterialPractice MaterialKind = "practico"
)

func (k MaterialKind) Valid() bool { return k == MaterialTheory || k == MaterialPractice }

// Material es lo que el estudiante tiene para estudiar una materia. El plan se
// arma a partir de este material real y no inventa teóricos ni prácticos.
type Material struct {
	ID        string
	StudentID string
	SubjectID string
	Kind      MaterialKind
	Title     string
	// Content es lo que tiene adentro: temas, ejercicios, páginas. Opcional;
	// la IA lo usa para decidir qué hacer en cada sesión.
	Content   string
	CreatedAt time.Time
}

type ExamStatus string

const (
	ExamScheduled ExamStatus = "programado"
	ExamCancelled ExamStatus = "cancelado"
)

type Exam struct {
	ID        string
	StudentID string
	SubjectID string
	Type      ExamType
	// StartsAt es el inicio del examen en la zona horaria del estudiante.
	// Si el estudiante no cargó hora, se toma el comienzo del día.
	StartsAt time.Time
	Topics   []string
	Status   ExamStatus
	// Modality: teórico, práctico, mixto o vacía si no se sabe.
	Modality ExamModality
	// MaterialIDs es el material de la materia que entra en el examen. Vacío:
	// entra todo el material cargado.
	MaterialIDs []string
	// PlanStatus es el estado del plan vigente del examen: confirmado o
	// desactualizado (RN-16). Vacío si todavía no tiene un plan confirmado.
	// Se completa al listar los exámenes; no se guarda en la fila del examen.
	PlanStatus PlanStatus
}

// Slot es una franja semanal en la que el estudiante puede estudiar.
// StartMinute y EndMinute son minutos desde la medianoche (18:00 = 1080).
type Slot struct {
	Weekday     time.Weekday
	StartMinute int
	EndMinute   int
}

type Availability struct {
	StudentID               string
	Slots                   []Slot
	MaxDailyMinutes         int
	PreferredSessionMinutes int
	// Location es la zona horaria del estudiante (por ejemplo
	// America/Argentina/Cordoba). Las fechas y horas de las sesiones se
	// interpretan en esta zona.
	Location *time.Location
}

// SessionDraft es una sesión propuesta: todavía no ocupa lugar en el
// calendario (RN-08). La generan el motor de reglas o el agente de IA.
type SessionDraft struct {
	Start           time.Time
	DurationMinutes int
	Topic           string
	Objective       string
	Reasoning       string
}

func (s SessionDraft) End() time.Time {
	return s.Start.Add(time.Duration(s.DurationMinutes) * time.Minute)
}

type SessionStatus string

const (
	SessionPending   SessionStatus = "pendiente"
	SessionCompleted SessionStatus = "completada"
	SessionPostponed SessionStatus = "pospuesta"
)

// Session es una sesión reservada en el calendario del estudiante.
type Session struct {
	ID        string
	PlanID    string
	StudentID string
	ExamID    string
	Start     time.Time
	End       time.Time
	Topic     string
	Objective string
	Status    SessionStatus
	// Outdated indica que el examen cambió de fecha después de confirmar el
	// plan (RN-16): la sesión sigue visible hasta que el estudiante replanifique.
	Outdated bool
}

// OccupiesCalendar indica si la sesión ocupa su horario. Una sesión pospuesta
// libera su horario (sección 6.3 del SPEC).
func (s Session) OccupiesCalendar() bool { return s.Status != SessionPostponed }

type PlanStatus string

const (
	PlanProposed  PlanStatus = "propuesto"
	PlanConfirmed PlanStatus = "confirmado"
	PlanDiscarded PlanStatus = "descartado"
	PlanOutdated  PlanStatus = "desactualizado"
	PlanCancelled PlanStatus = "cancelado"
)

// GeneratedBy indica quién armó la propuesta.
type GeneratedBy string

const (
	GeneratedByAI    GeneratedBy = "ia"
	GeneratedByRules GeneratedBy = "reglas"
)

// Plan es una propuesta de sesiones para un examen y, una vez confirmada,
// el conjunto de sesiones reservadas.
type Plan struct {
	ID          string
	StudentID   string
	ExamID      string
	Status      PlanStatus
	GeneratedBy GeneratedBy
	Warnings    []string
	Drafts      []SessionDraft
	CreatedAt   time.Time
}
