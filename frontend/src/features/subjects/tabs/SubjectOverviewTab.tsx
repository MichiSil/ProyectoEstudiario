import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { CalendarPlus, Sparkles } from 'lucide-react'
import { useSubjectContext } from '../context'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { EmptyState } from '@/components/ui/EmptyState'
import { ExamFormSheet } from '@/features/calendar/ExamFormSheet'
import { formatDateLong, formatDaysUntil, localDate } from '@/lib/format'
import { difficultyColorVar } from '@/lib/domain-ui'
import { ANTICIPATION_DAYS, DIFFICULTY_LABEL, EXAM_TYPE_LABEL } from '@/api/types'

export function SubjectOverviewTab() {
  const { subject } = useSubjectContext()
  const { exams, sessions } = usePlannerData()
  const navigate = useNavigate()
  const [examFormOpen, setExamFormOpen] = useState(false)

  const upcoming = exams
    .filter((e) => e.subjectId === subject.id && e.status === 'programado' && new Date(e.startsAt) >= new Date())
    .sort((a, b) => a.startsAt.localeCompare(b.startsAt))
  const examsWithPlan = new Set(sessions.map((s) => s.examId))

  return (
    <div className="flex flex-col gap-4">
      <Badge dotColor={difficultyColorVar(subject.difficulty)} className="w-fit">
        Dificultad: {DIFFICULTY_LABEL[subject.difficulty]}
      </Badge>

      <div className="grid grid-cols-2 gap-3">
        <InfoCard
          label={subject.professors.length > 1 ? 'Profesores/as' : 'Profesor/a'}
          value={subject.professors.length ? subject.professors.join(', ') : 'Sin asignar'}
        />
        <InfoCard label="Anticipación del plan" value={`${ANTICIPATION_DAYS[subject.difficulty]} días antes`} />
      </div>

      <div>
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-sm font-medium text-ink">Próximas fechas</h3>
          <Button size="sm" variant="ghost" onClick={() => setExamFormOpen(true)}>
            <CalendarPlus size={14} />
            Cargar fecha
          </Button>
        </div>
        {!upcoming.length ? (
          <EmptyState title="No hay fechas próximas para esta materia" />
        ) : (
          <div className="flex flex-col gap-2">
            {upcoming.map((exam) => {
              const date = localDate(exam.startsAt)
              return (
                <Card key={exam.id} className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-ink">{EXAM_TYPE_LABEL[exam.type]}</p>
                    <p className="text-xs text-muted">
                      {formatDateLong(date)} · {formatDaysUntil(date)}
                    </p>
                  </div>
                  {examsWithPlan.has(exam.id) ? (
                    <Badge tone="success">Plan confirmado</Badge>
                  ) : (
                    <Button size="sm" variant="secondary" onClick={() => navigate(`/plan?examen=${exam.id}`)}>
                      <Sparkles size={14} />
                      Pedir plan
                    </Button>
                  )}
                </Card>
              )
            })}
          </div>
        )}
      </div>

      <ExamFormSheet open={examFormOpen} onClose={() => setExamFormOpen(false)} defaultSubjectId={subject.id} />
    </div>
  )
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <p className="text-xs text-subtle">{label}</p>
      <p className="mt-0.5 truncate text-sm font-medium text-ink">{value}</p>
    </Card>
  )
}
