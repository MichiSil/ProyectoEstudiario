import { useState } from 'react'
import { NavLink, Outlet, useNavigate, useParams } from 'react-router-dom'
import clsx from 'clsx'
import { AlertTriangle, ChevronLeft, Pencil, Trash2 } from 'lucide-react'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { Button } from '@/components/ui/Button'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { plannerApi } from '@/api/planner'
import type { Subject } from '@/api/types'
import { subjectColor } from '@/lib/domain-ui'
import { SubjectFormSheet } from './SubjectFormSheet'

const TABS = [
  { to: '', label: 'Resumen' },
  { to: 'material', label: 'Material' },
  { to: 'plan', label: 'Plan' },
]

export function SubjectDetailScreen() {
  const { subjectId } = useParams<{ subjectId: string }>()
  const navigate = useNavigate()
  const { subjectsById, exams, loading, refresh } = usePlannerData()
  const subject = subjectId ? subjectsById.get(subjectId) : undefined
  const [editing, setEditing] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState<string | null>(null)

  if (loading) return <p className="p-6 text-sm text-muted">Cargando…</p>
  if (!subject) return <p className="p-6 text-sm text-muted">Materia no encontrada.</p>

  const activeExams = exams.filter((e) => e.subjectId === subject.id && e.status === 'programado')

  // Cambiar la dificultad no rehace los planes ya confirmados (RN-50): se avisa
  // para que el estudiante decida si los rehace.
  function onSaved(saved: Subject, before: Subject) {
    const withPlan = activeExams.filter((e) => e.planStatus === 'confirmado').length
    setNotice(
      saved.difficulty !== before.difficulty && withPlan > 0
        ? `Cambiaste la dificultad. Tus planes ya confirmados siguen igual: si querés que la tengan en cuenta, abrí el examen y tocá "Rehacer plan".`
        : null,
    )
  }

  async function remove() {
    setDeleting(true)
    setError(null)
    try {
      await plannerApi.deleteSubject(subject!.id)
      await refresh()
      navigate('/materias')
    } catch (err) {
      setError(err)
      setDeleting(false)
    }
  }

  return (
    <div>
      <div className="border-b border-border px-4 pb-4 pt-4">
        <div className="flex items-center gap-2">
          <button
            onClick={() => navigate('/materias')}
            aria-label="Volver a materias"
            className="rounded-full p-1.5 text-muted hover:bg-accent-soft"
          >
            <ChevronLeft size={20} />
          </button>
          <span className="h-3 w-3 rounded-full" style={{ backgroundColor: subjectColor(subject.id) }} />
          <h1 className="min-w-0 flex-1 truncate text-xl">{subject.name}</h1>
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)} aria-label="Editar materia">
            <Pencil size={15} />
            <span className="hidden sm:inline">Editar</span>
          </Button>
          <Button size="sm" variant="ghost" className="text-danger!" onClick={() => setConfirmingDelete(true)} aria-label="Borrar materia">
            <Trash2 size={15} />
            <span className="hidden sm:inline">Borrar</span>
          </Button>
        </div>

        {confirmingDelete && (
          <div className="mt-3 rounded-xl bg-danger/10 p-3">
            <p className="mb-3 text-sm text-ink">
              ¿Borrar <span className="font-medium">{subject.name}</span>?
              {activeExams.length > 0
                ? ` Se cancelan sus ${activeExams.length} examen${activeExams.length === 1 ? '' : 'es'} y se liberan las sesiones pendientes; las que ya completaste quedan como historial.`
                : ' No tiene exámenes cargados.'}
            </p>
            <div className="grid grid-cols-2 gap-2 sm:flex">
              <Button variant="secondary" onClick={() => setConfirmingDelete(false)} disabled={deleting}>
                Volver
              </Button>
              <Button variant="danger" onClick={() => void remove()} disabled={deleting}>
                {deleting ? 'Borrando…' : 'Sí, borrar'}
              </Button>
            </div>
            {!!error && (
              <div className="mt-3">
                <ApiErrorNotice error={error} />
              </div>
            )}
          </div>
        )}

        {notice && (
          <p className="mt-3 flex gap-2 rounded-xl bg-warning/10 px-3 py-2.5 text-sm text-ink">
            <AlertTriangle size={16} className="mt-0.5 shrink-0 text-warning" />
            {notice}
          </p>
        )}

        <div className="no-scrollbar mt-4 flex gap-1 overflow-x-auto">
          {TABS.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.to === ''}
              className={({ isActive }) =>
                clsx(
                  'shrink-0 rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors',
                  isActive ? 'bg-accent text-white' : 'text-muted hover:bg-accent-soft',
                )
              }
            >
              {tab.label}
            </NavLink>
          ))}
        </div>
      </div>

      <div className="px-4 py-5">
        <Outlet context={{ subject }} />
      </div>

      <SubjectFormSheet open={editing} subject={subject} onClose={() => setEditing(false)} onSaved={onSaved} />
    </div>
  )
}
