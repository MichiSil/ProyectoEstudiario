import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { AlertTriangle, BookOpen, Pencil, Sparkles, XCircle } from 'lucide-react'
import { Sheet } from '@/components/ui/Sheet'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { plannerApi } from '@/api/planner'
import { EXAM_TYPE_LABEL, MODALITY_LABEL, type Exam } from '@/api/types'
import { examTypeColorVar, subjectColor } from '@/lib/domain-ui'
import { formatDateLong, formatDaysUntil, formatTime, formatTimeRange, localDate } from '@/lib/format'
import { ExamFormSheet } from './ExamFormSheet'

export function ExamDetailSheet({ exam: selected, onClose }: { exam: Exam | null; onClose: () => void }) {
  const navigate = useNavigate()
  const location = useLocation()
  const { subjectsById, examsById, sessions, rememberExam, refreshSessions } = usePlannerData()
  const [editing, setEditing] = useState(false)
  const [confirmingCancel, setConfirmingCancel] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    setEditing(false)
    setConfirmingCancel(false)
    setError(null)
  }, [selected?.id])

  if (!selected) return null
  // Se muestra la versión más reciente del examen (puede haber cambiado al editarlo).
  const exam = examsById.get(selected.id) ?? selected

  if (editing) return <ExamFormSheet open exam={exam} onClose={() => setEditing(false)} />

  const subject = subjectsById.get(exam.subjectId)
  // Desde la página de la propia materia, "Ver materia" no lleva a ningún lado nuevo.
  const onSubjectPage = !!subject && location.pathname.startsWith(`/materias/${subject.id}`)
  // Cerrar el detalle antes de navegar: si no, queda abierto encima de la otra pantalla.
  function goTo(path: string) {
    onClose()
    navigate(path)
  }
  const plan = sessions.filter((s) => s.examId === exam.id).sort((a, b) => a.start.localeCompare(b.start))
  const date = localDate(exam.startsAt)
  const time = formatTime(exam.startsAt)
  const cancelled = exam.status === 'cancelado'
  const outdated = exam.planStatus === 'desactualizado'

  async function cancelExam() {
    setCancelling(true)
    setError(null)
    try {
      rememberExam(await plannerApi.cancelExam(exam.id))
      await refreshSessions()
      setConfirmingCancel(false)
    } catch (err) {
      setError(err)
    } finally {
      setCancelling(false)
    }
  }

  return (
    <Sheet open onClose={onClose} title={EXAM_TYPE_LABEL[exam.type]}>
      <div className="mb-4 flex items-center gap-2">
        <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: examTypeColorVar(exam.type) }} />
        <h3 className="text-lg font-medium text-ink">
          {EXAM_TYPE_LABEL[exam.type]}
          {subject ? ` de ${subject.name}` : ''}
        </h3>
      </div>

      <div className="mb-4 flex flex-wrap gap-2">
        {subject && (
          <button onClick={() => goTo(`/materias/${subject.id}`)}>
            <Badge tone="accent" dotColor={subjectColor(subject.id)}>
              {subject.name}
            </Badge>
          </button>
        )}
        <Badge tone="neutral">{formatDateLong(date)}</Badge>
        {time !== '00:00' && <Badge tone="neutral">{time}</Badge>}
        <Badge tone={cancelled ? 'danger' : 'neutral'}>{cancelled ? 'Cancelado' : formatDaysUntil(date)}</Badge>
        {exam.modality && <Badge tone="accent">{MODALITY_LABEL[exam.modality]}</Badge>}
      </div>

      {!!exam.topics.length && (
        <p className="mb-3 text-sm text-muted">
          <span className="font-medium text-ink">Temas: </span>
          {exam.topics.join(', ')}
        </p>
      )}

      {outdated && !cancelled && (
        <p className="mb-4 flex gap-2 rounded-xl bg-warning/10 px-3 py-2.5 text-sm text-ink">
          <AlertTriangle size={16} className="mt-0.5 shrink-0 text-warning" />
          El examen cambió de fecha y el plan quedó desactualizado. Sus sesiones siguen en el calendario hasta que pidas
          uno nuevo.
        </p>
      )}

      {!!plan.length && (
        <div className="mb-4">
          <p className="mb-2 text-sm font-medium text-ink">
            {outdated ? 'Plan de estudio desactualizado' : 'Plan de estudio confirmado'}
          </p>
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

      {!!error && (
        <div className="mb-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      <div className="flex flex-col gap-2">
        {!cancelled && (!plan.length || outdated) && (
          <Button onClick={() => goTo(`/plan?examen=${exam.id}`)}>
            <Sparkles size={16} />
            {outdated ? 'Replanificar' : 'Pedir plan de estudio'}
          </Button>
        )}
        {subject && !onSubjectPage && (
          <Button variant="secondary" onClick={() => goTo(`/materias/${subject.id}`)}>
            <BookOpen size={16} />
            Ver materia
          </Button>
        )}
        {!cancelled && (
          <Button variant="secondary" onClick={() => setEditing(true)}>
            <Pencil size={16} />
            Editar examen
          </Button>
        )}
        {!cancelled &&
          (confirmingCancel ? (
            <div className="rounded-xl bg-danger/10 p-3">
              <p className="mb-3 text-sm text-ink">
                ¿Cancelar este examen? Se liberan sus sesiones pendientes; las que ya completaste quedan como historial.
              </p>
              <div className="grid grid-cols-2 gap-2">
                <Button variant="secondary" onClick={() => setConfirmingCancel(false)} disabled={cancelling}>
                  Volver
                </Button>
                <Button variant="danger" onClick={() => void cancelExam()} disabled={cancelling}>
                  {cancelling ? 'Cancelando…' : 'Sí, cancelar'}
                </Button>
              </div>
            </div>
          ) : (
            <Button variant="ghost" className="text-danger!" onClick={() => setConfirmingCancel(true)}>
              <XCircle size={16} />
              Cancelar examen
            </Button>
          ))}
      </div>
    </Sheet>
  )
}
