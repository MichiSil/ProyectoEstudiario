// Formas de la API de planner-service, tal como las devuelve
// services/planner-service/internal/adapters/httpapi/handler.go.
// Si cambia un DTO del backend, se actualiza acá.

/** 1 fácil, 2 media, 3 difícil, 4 muy difícil (glosario del SPEC). */
export type Difficulty = 1 | 2 | 3 | 4

export const DIFFICULTY_LABEL: Record<Difficulty, string> = {
  1: 'Fácil',
  2: 'Media',
  3: 'Difícil',
  4: 'Muy difícil',
}

/** Días de anticipación según la dificultad (RN-06). Sólo se usan para explicar el plan. */
export const ANTICIPATION_DAYS: Record<Difficulty, number> = { 1: 5, 2: 10, 3: 15, 4: 21 }

export interface Subject {
  id: string
  name: string
  professors: string[]
  difficulty: Difficulty
}

export type ExamType = 'parcial' | 'final' | 'recuperatorio' | 'entrega'

export const EXAM_TYPE_LABEL: Record<ExamType, string> = {
  parcial: 'Parcial',
  final: 'Final',
  recuperatorio: 'Recuperatorio',
  entrega: 'Entrega',
}

export type ExamStatus = 'programado' | 'cancelado'

export interface Exam {
  id: string
  subjectId: string
  type: ExamType
  /** RFC 3339 con zona horaria */
  startsAt: string
  topics: string[]
  status: ExamStatus
  /** Plan vigente: confirmado, o desactualizado si cambió la fecha (RN-16). Ausente si no tiene plan. */
  planStatus?: 'confirmado' | 'desactualizado'
}

export interface WeeklySlot {
  /** 0 = domingo */
  weekday: number
  startTime: string // HH:mm
  endTime: string // HH:mm
}

export interface Availability {
  weeklySlots: WeeklySlot[]
  maxDailyMinutes: number
  preferredSessionMinutes: number
  /** zona IANA, por ejemplo America/Argentina/Cordoba */
  timezone: string
}

export interface SessionDraft {
  start: string
  end: string
  durationMinutes: number
  topic: string
  objective: string
  reasoning: string
}

export type GeneratedBy = 'ia' | 'reglas'

export interface PlanProposal {
  id: string
  examId: string
  status: 'propuesto'
  generatedBy: GeneratedBy
  warnings: string[]
  sessions: SessionDraft[] | null
}

export type SessionStatus = 'pendiente' | 'completada' | 'pospuesta'

export interface Session {
  id: string
  planId: string
  examId: string
  start: string
  end: string
  topic: string
  objective: string
  status: SessionStatus
  /** El examen cambió de fecha después de confirmar el plan (RN-16). */
  outdated: boolean
}

export interface ConfirmResult {
  planId: string
  status: 'confirmado'
  sessions: Session[]
}

/** Regla incumplida, con el identificador del SPEC (por ejemplo RN-12). */
export interface RuleViolation {
  rule: string
  message: string
  /** -1 si no corresponde a una sesión puntual */
  sessionIndex: number
}

// Formas de la API de users-service
// (services/users-service/internal/controller/http.go).

export type UserRole = 'estudiante' | 'admin'

export interface User {
  id: string
  name: string
  email: string
  role: UserRole
  createdAt: string
}

export interface LoginResult {
  /** JWT HS256 que valida el api-gateway */
  token: string
  expiresAt: string
  user: User
}

/** Error de validación de un campo (VALIDATION_ERROR de users-service). */
export interface FieldError {
  field: string
  message: string
}
