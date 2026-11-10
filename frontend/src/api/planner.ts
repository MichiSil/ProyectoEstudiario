import { request } from './client'
import type { Availability, ConfirmResult, Difficulty, Exam, ExamModality, ExamType, Material, MaterialKind, PlanProposal, Session, SessionStatus, Subject } from './types'

// Endpoints de planner-service publicados en el api-gateway bajo /api/planner.

const base = '/planner'

export const plannerApi = {
  listSubjects: () => request<Subject[]>(`${base}/subjects`),

  createSubject: (input: { name: string; professors: string[]; difficulty: Difficulty }) =>
    request<Subject>(`${base}/subjects`, { method: 'POST', body: input }),

  /** RF-04: sólo se mandan los campos que cambian. No toca los planes ya confirmados (RN-50). */
  updateSubject: (subjectId: string, changes: { name?: string; professors?: string[]; difficulty?: Difficulty }) =>
    request<Subject>(`${base}/subjects/${encodeURIComponent(subjectId)}`, { method: 'PATCH', body: changes }),

  /** RF-04: cancela sus exámenes y libera sus sesiones pendientes; las completadas quedan como historial. */
  deleteSubject: (subjectId: string) =>
    request<void>(`${base}/subjects/${encodeURIComponent(subjectId)}`, { method: 'DELETE' }),

  listMaterials: (subjectId: string) => request<Material[]>(`${base}/subjects/${encodeURIComponent(subjectId)}/materials`),

  createMaterial: (subjectId: string, input: { kind: MaterialKind; title: string; content: string }) =>
    request<Material>(`${base}/subjects/${encodeURIComponent(subjectId)}/materials`, { method: 'POST', body: input }),

  deleteMaterial: (materialId: string) =>
    request<void>(`${base}/materials/${encodeURIComponent(materialId)}`, { method: 'DELETE' }),

  createExam: (input: {
    subjectId: string
    type: ExamType
    startsAt: string
    topics: string[]
    modality: ExamModality
    materialIds: string[]
  }) =>
    request<Exam>(`${base}/exams`, { method: 'POST', body: input }),

  listExams: () => request<Exam[]>(`${base}/exams`),

  /** RF-06: sólo se mandan los campos que cambian. Cambiar la fecha desactualiza el plan (RN-16). */
  updateExam: (
    examId: string,
    changes: { type?: ExamType; startsAt?: string; topics?: string[]; modality?: ExamModality; materialIds?: string[] },
  ) =>
    request<Exam>(`${base}/exams/${encodeURIComponent(examId)}`, { method: 'PATCH', body: changes }),

  /** RF-06: libera las sesiones pendientes y conserva las completadas (RN-17). */
  cancelExam: (examId: string) =>
    request<Exam>(`${base}/exams/${encodeURIComponent(examId)}/cancel`, { method: 'POST' }),

  /** RF-07: la disponibilidad guardada. 404 AVAILABILITY_NOT_SET si todavía no la cargó. */
  getAvailability: () => request<Availability>(`${base}/availability`),

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

  /**
   * RF-11: completar (desde que empieza, RN-48), posponer (libera el horario y
   * desactualiza el plan, RN-49) o volver a pendiente una completada.
   */
  setSessionStatus: (sessionId: string, status: SessionStatus) =>
    request<Session>(`${base}/sessions/${encodeURIComponent(sessionId)}`, { method: 'PATCH', body: { status } }),

  /** RF-06: rehacer el plan aunque el examen no haya cambiado: lo deja desactualizado para pedir otro. */
  replan: (examId: string) =>
    request<Exam>(`${base}/exams/${encodeURIComponent(examId)}/replan`, { method: 'POST' }),
}
