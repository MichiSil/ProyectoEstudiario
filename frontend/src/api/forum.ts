import { downloadFile, request } from './client'
import type { ForumPage, Summary } from './types'

// Endpoints de forum-service publicados en el api-gateway bajo /api/forum.
// Las rutas /admin exigen rol admin (el gateway lo manda en X-User-Role).

const base = '/forum'

export type SummarySort = 'relevance' | 'rating' | 'date'

export const forumApi = {
  /** RF-19 y RF-20: aprobados, con texto, materia, calificación mínima y orden. */
  search: (params: { q?: string; category?: string; minRating?: number; sort?: SummarySort; page?: number }) => {
    const qs = new URLSearchParams()
    if (params.q) qs.set('q', params.q)
    if (params.category) qs.set('category', params.category)
    if (params.minRating) qs.set('minRating', String(params.minRating))
    if (params.sort) qs.set('sort', params.sort)
    qs.set('page', String(params.page ?? 1))
    qs.set('size', '20')
    return request<ForumPage>(`${base}/summaries?${qs}`)
  },

  /** RF-15: mis publicaciones con su estado y, si fue rechazada, el motivo. */
  mine: () => request<ForumPage>(`${base}/summaries/mine?size=50`),

  /** RF-14: título, materia, descripción, etiquetas y el PDF (RN-20: hasta 10 MB). */
  publish: (input: { title: string; category: string; description: string; tags: string[]; file: File }) => {
    const form = new FormData()
    form.set('title', input.title)
    form.set('category', input.category)
    form.set('description', input.description)
    form.set('tags', input.tags.join(','))
    form.set('file', input.file)
    return request<Summary>(`${base}/summaries`, { method: 'POST', body: form })
  },

  /** RF-22: de 1 a 5; volver a calificar reemplaza el puntaje anterior (RN-30). */
  rate: (id: string, score: number) =>
    request<Summary>(`${base}/summaries/${encodeURIComponent(id)}/rating`, { method: 'PUT', body: { score } }),

  download: (summary: Summary) => downloadFile(summary.file.downloadPath.replace(/^\/api/, ''), `${summary.title}.pdf`),

  // ---------- Moderación (sólo admin) ----------

  /** RF-16: pendientes, de la más vieja a la más nueva. */
  pending: () => request<ForumPage>(`${base}/admin/summaries/pending?size=50`),

  approve: (id: string) => request<Summary>(`${base}/admin/summaries/${encodeURIComponent(id)}/approve`, { method: 'POST' }),

  /** RN-22: rechazar exige un motivo. */
  reject: (id: string, reason: string) =>
    request<Summary>(`${base}/admin/summaries/${encodeURIComponent(id)}/reject`, { method: 'POST', body: { reason } }),

  /** RF-18: en cualquier estado. */
  remove: (id: string) => request<void>(`${base}/admin/summaries/${encodeURIComponent(id)}`, { method: 'DELETE' }),
}
