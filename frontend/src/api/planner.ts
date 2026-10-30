import { request } from './client'
import type { Availability, ConfirmResult, Difficulty, Exam, ExamType, PlanProposal, Session, Subject } from './types'

// Endpoints de planner-service publicados en el api-gateway bajo /api/planner.

const base = '/planner'

export const plannerApi = {
  listSubjects: () => request<Subject[]>(`${base}/subjects`),

  createSubject: (input: { name: string; professors: string[]; difficulty: Difficulty }) =>
    request<Subject>(`${base}/subjects`, { method: 'POST', body: input }),

  createExam: (input: { subjectId: string; type: ExamType; startsAt: string; topics: string[] }) =>
    request<Exam>(`${base}/exams`, { method: 'POST', body: input }),

  listExams: () => request<Exam[]>(`${base}/exams`),

  /** RF-06: sólo se mandan los campos que cambian. Cambiar la fecha desactualiza el plan (RN-16). */
  updateExam: (examId: string, changes: { type?: ExamType; startsAt?: string; topics?: string[] }) =>
    request<Exam>(`${base}/exams/${encodeURIComponent(examId)}`, { method: 'PATCH', body: changes }),

  /** RF-06: libera las sesiones pendientes y conserva las completadas (RN-17). */
  cancelExam: (examId: string) =>
    request<Exam>(`${base}/exams/${encodeURIComponent(examId)}/cancel`, { method: 'POST' }),

  setAvailability: (input: Availability) =>
    request<void>(`${base}/availability`, { method: 'PUT', body: input }),

  /** RF-08: la IA (o el motor de reglas, RN-09) propone sesiones. No reserva nada (RN-08). */
  proposePlan: (examId: string) =>
    request<PlanProposal>(`${base}/exams/${encodeURIComponent(examId)}/proposals`, { method: 'POST' }),

  /**
   * RF-09, acción principal. La misma Idempotency-Key en un reintento devuelve
   * el mismo resultado sin duplicar sesiones (RN-14).
   */
  confirmPlan: (planId: string, idempotencyKey: string) =>
    request<ConfirmResult>(`${base}/plans/${encodeURIComponent(planId)}/confirm`, {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
    }),

  listSessions: () => request<Session[]>(`${base}/sessions`),
}
