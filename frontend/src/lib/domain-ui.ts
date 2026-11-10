import type { Difficulty, ExamType, Session, SessionStatus } from '@/api/types'

export function difficultyColorVar(level: Difficulty): string {
  return {
    1: 'var(--difficulty-easy)',
    2: 'var(--difficulty-medium)',
    3: 'var(--difficulty-hard)',
    4: 'var(--difficulty-extreme)',
  }[level]
}

export function examTypeColorVar(type: ExamType): string {
  switch (type) {
    case 'final':
      return 'var(--event-final)'
    case 'entrega':
      return 'var(--event-delivery)'
    case 'parcial':
    case 'recuperatorio':
    default:
      return 'var(--event-exam)'
  }
}

export const SESSION_COLOR = 'var(--event-reminder)'

export const SESSION_STATUS_LABEL: Record<SessionStatus, string> = {
  pendiente: 'Pendiente',
  completada: 'Completada',
  pospuesta: 'Pospuesta',
}

const SUBJECT_COLORS = ['#355c7d', '#7fa58a', '#e7a64a', '#d35d5d', '#5c7d8a', '#8a7a9b', '#4c7a52', '#a2532b']

/**
 * Color de una materia. planner-service no guarda color, así que se deriva
 * del id: es estable entre dispositivos y no hace falta persistirlo.
 */
export function subjectColor(subjectId: string): string {
  let hash = 0
  for (const ch of subjectId) hash = (hash * 31 + ch.charCodeAt(0)) | 0
  return SUBJECT_COLORS[Math.abs(hash) % SUBJECT_COLORS.length]
}

export const WEEKDAY_LABEL = ['Domingo', 'Lunes', 'Martes', 'Miércoles', 'Jueves', 'Viernes', 'Sábado']

/**
 * En qué momento está una sesión, para mostrarla y saber qué se puede hacer:
 * - futura: todavía no empezó, no se puede completar (RN-48);
 * - en curso: ya empezó y no terminó;
 * - no hecha: terminó y sigue pendiente;
 * - completada o pospuesta.
 */
export type SessionPhase = 'futura' | 'en-curso' | 'no-hecha' | 'completada' | 'pospuesta'

export function sessionPhase(session: Session, now = new Date()): SessionPhase {
  if (session.status === 'completada') return 'completada'
  if (session.status === 'pospuesta') return 'pospuesta'
  if (new Date(session.start) > now) return 'futura'
  if (new Date(session.end) > now) return 'en-curso'
  return 'no-hecha'
}
