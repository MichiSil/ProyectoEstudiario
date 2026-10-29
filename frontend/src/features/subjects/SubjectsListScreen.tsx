import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { NotebookText, Plus } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { EmptyState } from '@/components/ui/EmptyState'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { DIFFICULTY_LABEL } from '@/api/types'
import { difficultyColorVar, subjectColor } from '@/lib/domain-ui'
import { SubjectFormSheet } from './SubjectFormSheet'

export function SubjectsListScreen() {
  const { subjects, exams, loading, error } = usePlannerData()
  const [searchParams, setSearchParams] = useSearchParams()
  const [formOpen, setFormOpen] = useState(false)

  useEffect(() => {
    if (searchParams.get('nueva')) {
      setFormOpen(true)
      setSearchParams({}, { replace: true })
    }
  }, [searchParams, setSearchParams])

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <div className="flex items-center justify-between">
        <h1 className="text-[28px] leading-tight">Materias</h1>
        <Button size="icon" onClick={() => setFormOpen(true)} aria-label="Nueva materia">
          <Plus size={18} />
        </Button>
      </div>

      {error && (
        <div className="mt-5">
          <ApiErrorNotice error={new Error(error)} />
        </div>
      )}

      {loading ? (
        <p className="mt-6 text-sm text-muted">Cargando…</p>
      ) : !subjects.length ? (
        <div className="mt-6">
          <EmptyState
            icon={<NotebookText size={20} />}
            title="Todavía no cargaste materias"
            description="Creá tu primera materia para empezar a organizar parciales, entregas y plan de estudio."
            action={<Button onClick={() => setFormOpen(true)}>Nueva materia</Button>}
          />
        </div>
      ) : (
        <div className="mt-5 flex flex-col gap-2">
          {subjects.map((subject) => {
            const upcoming = exams.filter(
              (e) => e.subjectId === subject.id && e.status === 'programado' && new Date(e.startsAt) >= new Date(),
            ).length
            return (
              <Link
                key={subject.id}
                to={`/materias/${subject.id}`}
                className="animate-fade-in flex items-center gap-3 rounded-2xl border border-border bg-surface p-4 hover:border-accent/40"
              >
                <span className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: subjectColor(subject.id) }} />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium text-ink">{subject.name}</p>
                  <p className="truncate text-sm text-muted">
                    {subject.professors.length ? subject.professors.join(', ') : 'Sin profesor asignado'}
                  </p>
                </div>
                <div className="flex shrink-0 flex-col items-end gap-1">
                  <Badge dotColor={difficultyColorVar(subject.difficulty)}>{DIFFICULTY_LABEL[subject.difficulty]}</Badge>
                  {upcoming > 0 && (
                    <span className="text-xs text-subtle">
                      {upcoming} fecha{upcoming === 1 ? '' : 's'} próxima{upcoming === 1 ? '' : 's'}
                    </span>
                  )}
                </div>
              </Link>
            )
          })}
        </div>
      )}

      <SubjectFormSheet open={formOpen} onClose={() => setFormOpen(false)} />
    </div>
  )
}
