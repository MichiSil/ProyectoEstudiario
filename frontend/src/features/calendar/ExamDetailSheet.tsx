import { useNavigate } from 'react-router-dom'
import { BookOpen, Sparkles } from 'lucide-react'
import { Sheet } from '@/components/ui/Sheet'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { EXAM_TYPE_LABEL, type Exam } from '@/api/types'
import { examTypeColorVar, subjectColor } from '@/lib/domain-ui'
import { formatDateLong, formatDaysUntil, formatTime, formatTimeRange, localDate } from '@/lib/format'

export function ExamDetailSheet({ exam, onClose }: { exam: Exam | null; onClose: () => void }) {
  const navigate = useNavigate()
  const { subjectsById, sessions } = usePlannerData()

  if (!exam) return null
  const subject = subjectsById.get(exam.subjectId)
  const plan = sessions.filter((s) => s.examId === exam.id).sort((a, b) => a.start.localeCompare(b.start))
  const date = localDate(exam.startsAt)
  const time = formatTime(exam.startsAt)

  return (
    <Sheet open={!!exam} onClose={onClose} title={EXAM_TYPE_LABEL[exam.type]}>
      <div className="mb-4 flex items-center gap-2">
        <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: examTypeColorVar(exam.type) }} />
        <h3 className="text-lg font-medium text-ink">
          {EXAM_TYPE_LABEL[exam.type]}
          {subject ? ` de ${subject.name}` : ''}
        </h3>
      </div>

      <div className="mb-4 flex flex-wrap gap-2">
        {subject && (
          <button onClick={() => navigate(`/materias/${subject.id}`)}>
            <Badge tone="accent" dotColor={subjectColor(subject.id)}>
              {subject.name}
            </Badge>
          </button>
        )}
        <Badge tone="neutral">{formatDateLong(date)}</Badge>
        {time !== '00:00' && <Badge tone="neutral">{time}</Badge>}
        <Badge tone={exam.status === 'cancelado' ? 'danger' : 'neutral'}>
          {exam.status === 'cancelado' ? 'Cancelado' : formatDaysUntil(date)}
        </Badge>
      </div>

      {!!exam.topics.length && (
        <p className="mb-3 text-sm text-muted">
          <span className="font-medium text-ink">Temas: </span>
          {exam.topics.join(', ')}
        </p>
      )}

      {!!plan.length && (
        <div className="mb-4">
          <p className="mb-2 text-sm font-medium text-ink">Plan de estudio confirmado</p>
          <div className="flex flex-col gap-1.5">
            {plan.map((s) => (
              <div key={s.id} className="flex items-center justify-between gap-3 rounded-xl bg-paper px-3 py-2 text-sm">
                <span className="min-w-0 truncate text-ink">{s.objective || s.topic}</span>
                <span className="shrink-0 text-xs text-subtle">
                  {formatDateLong(localDate(s.start))} · {formatTimeRange(s.start, s.end)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex flex-col gap-2">
        {subject && (
          <Button variant="secondary" onClick={() => navigate(`/materias/${subject.id}`)}>
            <BookOpen size={16} />
            Ver materia
          </Button>
        )}
        {!plan.length && exam.status === 'programado' && (
          <Button onClick={() => navigate(`/plan?examen=${exam.id}`)}>
            <Sparkles size={16} />
            Pedir plan de estudio
          </Button>
        )}
      </div>
    </Sheet>
  )
}
