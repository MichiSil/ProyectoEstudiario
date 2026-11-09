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
  /** Cómo se evalúa; vacía si no se indicó (el plan no supone teoría ni ejercicios). */
  modality: ExamModality
  /** Material de la materia que entra; vacío = todo el material cargado. */
  materialIds: string[]
}

export type ExamModality = '' | 'teorico' | 'practico' | 'mixto'

export const MODALITY_LABEL: Record<ExamModality, string> = {
  '': 'No sé todavía',
  teorico: 'Teórico',
  practico: 'Práctico',
  mixto: 'Teórico y práctico',
}

export type MaterialKind = 'teorico' | 'practico'

/** Teórico o práctico que el estudiante cargó en una materia. El plan se arma con esto. */
export interface Material {
  id: string
  subjectId: string
  kind: MaterialKind
  title: string
  /** Qué tiene adentro (temas, ejercicios, páginas). Opcional. */
  content: string
  createdAt: string
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

/** Club de Beneficios del Grupo 1 (RF-29, ADR-015): GET y POST /api/users/me/club. */
export type ClubLinkStatus = 'sin_vincular' | 'pendiente' | 'vinculada'

export interface ClubMovement {
  id: string
  points: number
  reason: string
  occurredAt: string
}

export interface ClubStatus {
  status: ClubLinkStatus
  /** Saldo total en el Club; null si no está vinculada o el Club no respondió */
  balance: number | null
  /** Puntos ganados en Estudiario que todavía no se acreditaron */
  pendingPoints: number
  movements: ClubMovement[]
  /** CLUB_UNAVAILABLE si el Club no respondió: se muestra el resto igual */
  warning: 'CLUB_UNAVAILABLE' | null
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

// Formas de la API de forum-service (services/forum-service/internal/controller/common.go).

export type SummaryStatus = 'pendiente' | 'aprobado' | 'rechazado' | 'eliminado'

export interface Summary {
  id: string
  title: string
  description: string
  /** La materia, como texto libre (RN-25) */
  category: string
  tags: string[]
  status: SummaryStatus
  rejectionReason: string | null
  /** estudiario, o el consumidor de la capacidad que lo registró. 'externo' es el valor viejo (ADR-014). */
  source: 'estudiario' | 'agencia_de_viajes' | 'externo'
  author: { name: string; externalId?: string }
  rating: { average: number | null; count: number }
  /** publicUrl: ruta para ver el PDF sin login, sólo en los documentos externos (ADR-014). */
  file: { contentType: string; sizeBytes: number; downloadPath: string; publicUrl: string | null }
  createdAt: string
  publishedAt: string | null
}

export interface ForumPage {
  items: Summary[]
  page: number
  size: number
  total: number
}
