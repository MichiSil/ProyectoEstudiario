import { CheckCircle2, Info } from 'lucide-react'
import { useSubjectContext } from '../context'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { EmptyState } from '@/components/ui/EmptyState'
import { Card } from '@/components/ui/Card'
import { SessionCard } from '@/features/planner/SessionCard'

export function SubjectPlanTab() {
  const { subject } = useSubjectContext()
  const { sessions, examsById } = usePlannerData()

  const own = sessions
    .filter((s) => examsById.get(s.examId)?.subjectId === subject.id)
    .sort((a, b) => a.start.localeCompare(b.start))
  const pending = own.filter((s) => s.status !== 'completada')
  const completed = own.filter((s) => s.status === 'completada')

  if (!own.length) {
    return (
      <EmptyState
        icon={<Info size={24} />}
        title="Todavía no hay un plan de estudio"
        description="Cargá un parcial o una entrega de esta materia y pedí la propuesta en Plan."
      />
    )
  }

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="mb-2 text-sm font-medium text-ink">Pendientes</h3>
        <div className="flex flex-col gap-2">
          {pending.map((s) => (
            <SessionCard key={s.id} session={s} />
          ))}
        </div>
      </div>

      {!!completed.length && (
        <div>
          <h3 className="mb-2 text-sm font-medium text-ink">Completadas</h3>
          <div className="flex flex-col gap-2">
            {completed.map((s) => (
              <Card key={s.id} className="opacity-60">
                <div className="flex items-center gap-3">
                  <CheckCircle2 size={20} className="shrink-0 text-success" aria-hidden="true" />
                  <p className="truncate text-sm text-ink">{s.objective || s.topic}</p>
                </div>
              </Card>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
