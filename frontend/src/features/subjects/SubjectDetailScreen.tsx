import { NavLink, Outlet, useNavigate, useParams } from 'react-router-dom'
import clsx from 'clsx'
import { ChevronLeft } from 'lucide-react'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { subjectColor } from '@/lib/domain-ui'

const TABS = [
  { to: '', label: 'Resumen' },
  { to: 'plan', label: 'Plan' },
]

export function SubjectDetailScreen() {
  const { subjectId } = useParams<{ subjectId: string }>()
  const navigate = useNavigate()
  const { subjectsById, loading } = usePlannerData()
  const subject = subjectId ? subjectsById.get(subjectId) : undefined

  if (loading) return <p className="p-6 text-sm text-muted">Cargando…</p>
  if (!subject) return <p className="p-6 text-sm text-muted">Materia no encontrada.</p>

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
        </div>

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
    </div>
  )
}
