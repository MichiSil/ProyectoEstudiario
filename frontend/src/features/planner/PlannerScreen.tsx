import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { CalendarPlus, CheckCircle2, ChevronDown, ChevronUp, Pencil, Settings, Sparkles } from 'lucide-react'
import { plannerApi } from '@/api/planner'
import { ApiError } from '@/api/client'
import { EXAM_TYPE_LABEL, type Availability, type Exam, type PlanProposal, type Session } from '@/api/types'
import { ExamDetailSheet } from '@/features/calendar/ExamDetailSheet'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { Card } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { EmptyState } from '@/components/ui/EmptyState'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { formatDateLong, formatDaysUntil, localDate } from '@/lib/format'
import { ProposalCard } from './ProposalCard'
import { SessionCard } from './SessionCard'

/**
 * Plan de estudio: el estudiante pide una propuesta para un examen (RF-08), la
 * revisa y la confirma (RF-09, acción principal). La Idempotency-Key se genera
 * una vez por propuesta y se reusa en cada reintento, así un reintento de red
 * o un doble clic nunca duplica sesiones (RN-14).
 */
export function PlannerScreen() {
  const { exams, sessions, availability, subjectsById, examsById, refresh, refreshSessions, rememberExam } = usePlannerData()
  const [searchParams, setSearchParams] = useSearchParams()

  const [proposal, setProposal] = useState<PlanProposal | null>(null)
  const [requestingId, setRequestingId] = useState<string | null>(null)
  const [requestError, setRequestError] = useState<unknown>(null)
  const [requestedExamId, setRequestedExamId] = useState<string | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [confirmError, setConfirmError] = useState<unknown>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [detailExam, setDetailExam] = useState<Exam | null>(null)
  const idempotencyKeys = useRef(new Map<string, string>())

  const examsWithPlan = useMemo(() => new Set(sessions.map((s) => s.examId)), [sessions])
  const plannableExams = useMemo(
    () =>
      exams
        // Sin plan, o con el plan desactualizado porque cambió la fecha (RN-16): se puede replanificar.
        .filter(
          (e) =>
            e.status === 'programado' &&
            new Date(e.startsAt) > new Date() &&
            (e.planStatus === 'desactualizado' || (!e.planStatus && !examsWithPlan.has(e.id))),
        )
        .sort((a, b) => a.startsAt.localeCompare(b.startsAt)),
    [exams, examsWithPlan],
  )

  // Tres secciones, según el estado de cada sesión (se recalculan al marcarla o desmarcarla):
  // - próximas: pendientes que todavía no terminaron, de la más cercana a la más lejana;
  // - expiradas: terminaron sin completarse, y las pospuestas, de la más nueva a la más vieja;
  // - completadas: todas las que tienen el tick (también las adelantadas, RN-48), de la más nueva a la más vieja.
  const [showExpired, setShowExpired] = useState(false)
  const [showCompleted, setShowCompleted] = useState(false)
  const { upcoming, expired, completed } = useMemo(() => {
    const now = new Date()
    const upcoming: Session[] = []
    const expired: Session[] = []
    const completed: Session[] = []
    for (const s of sessions) {
      if (s.status === 'completada') completed.push(s)
      else if (s.status === 'pospuesta' || new Date(s.end) <= now) expired.push(s)
      else upcoming.push(s)
    }
    upcoming.sort((a, b) => a.start.localeCompare(b.start))
    expired.sort((a, b) => b.start.localeCompare(a.start))
    completed.sort((a, b) => b.start.localeCompare(a.start))
    return { upcoming, expired, completed }
  }, [sessions])
  const expiredPending = expired.filter((s) => s.status === 'pendiente').length

  async function requestProposal(examId: string) {
    setRequestingId(examId)
    setRequestedExamId(examId)
    setRequestError(null)
    setConfirmError(null)
    setMessage(null)
    try {
      setProposal(await plannerApi.proposePlan(examId))
    } catch (err) {
      setRequestError(err)
    } finally {
      setRequestingId(null)
    }
  }

  // Llegar desde un examen (?examen=<id>) pide la propuesta directamente.
  const autoRequested = useRef(false)
  useEffect(() => {
    const examId = searchParams.get('examen')
    if (!examId || autoRequested.current) return
    autoRequested.current = true
    setSearchParams({}, { replace: true })
    void requestProposal(examId)
  }, [searchParams, setSearchParams])

  async function confirm() {
    if (!proposal) return
    let key = idempotencyKeys.current.get(proposal.id)
    if (!key) {
      key = crypto.randomUUID()
      idempotencyKeys.current.set(proposal.id, key)
    }
    setConfirming(true)
    setConfirmError(null)
    try {
      const result = await plannerApi.confirmPlan(proposal.id, key)
      const exam = examsById.get(proposal.examId)
      if (exam) rememberExam({ ...exam, planStatus: 'confirmado' })
      setProposal(null)
      setMessage(`Listo: reservamos ${result.sessions.length} sesión${result.sessions.length === 1 ? '' : 'es'} en tu calendario.`)
      await refreshSessions()
    } catch (err) {
      setConfirmError(err)
      if (err instanceof ApiError && (err.code === 'PLAN_ALREADY_CONFIRMED' || err.code === 'EXAM_CANCELLED')) await refresh()
    } finally {
      setConfirming(false)
    }
  }

  function discard() {
    setProposal(null)
    setConfirmError(null)
  }

  const subjectNameOf = (session: Session) => {
    const exam = examsById.get(session.examId)
    return exam ? subjectsById.get(exam.subjectId)?.name : undefined
  }

  const proposalExam = proposal ? examsById.get(proposal.examId) : undefined
  const missingData = requestError instanceof ApiError && requestError.status === 404
  const requestedExam = requestedExamId ? examsById.get(requestedExamId) : undefined
  // planner-service responde 500 cuando no hay horarios libres antes del examen (ni la IA ni las reglas
  // encuentran lugar). Se explica con lo que sabemos de la disponibilidad guardada.
  const noSlotsHint =
    requestError instanceof ApiError && requestError.status === 500
      ? availability && requestedExam && !hasSlotBefore(availability, requestedExam.startsAt)
        ? 'Entre hoy y el examen no tenés ningún horario disponible. Agregá franjas en tu disponibilidad para los próximos días o cargá el examen con más margen.'
        : 'No pudimos armar el plan. Lo más común es que no queden horarios libres antes del examen: agregá franjas en tu disponibilidad o probá con un examen más adelante.'
      : undefined

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <h1 className="text-[28px] leading-tight">Plan de estudio</h1>
      <p className="mt-1 text-sm text-muted">
        Un agente de IA arma sesiones concretas a partir de tus exámenes, la dificultad de cada materia y tus horas
        disponibles. Vos lo revisás y lo confirmás.
      </p>

      {!availability && (
        <Card className="mt-5 flex items-center justify-between gap-3 bg-warning/10">
          <p className="text-sm text-ink">Para que el agente pueda planificar, contanos cuándo podés estudiar.</p>
          <Link to="/configuracion">
            <Button size="sm" variant="secondary">
              <Settings size={14} />
              Disponibilidad
            </Button>
          </Link>
        </Card>
      )}

      {message && (
        <p className="animate-fade-in mt-5 flex items-center gap-2 rounded-xl bg-success/10 px-4 py-3 text-sm text-ink">
          <CheckCircle2 size={16} className="shrink-0 text-success" />
          {message}
        </p>
      )}

      {!!requestError && (
        <div className="mt-5">
          {/* planner-service responde 404 cuando falta la disponibilidad (o el examen): se explica qué hacer. */}
          <ApiErrorNotice
            error={
              missingData
                ? new ApiError(404, 'NOT_FOUND', 'Todavía no guardaste tu disponibilidad, así que el agente no puede armar el plan.', [], requestError.correlationId)
                : requestError
            }
            hint={noSlotsHint}
          />
          {(missingData || noSlotsHint) && (
            <Link to="/configuracion" className="mt-3 inline-block">
              <Button size="sm">
                <Settings size={14} />
                {missingData ? 'Cargar mi disponibilidad' : 'Revisar mi disponibilidad'}
              </Button>
            </Link>
          )}
        </div>
      )}

      {proposal && (
        <section className="mt-5">
          <ProposalCard
            proposal={proposal}
            exam={proposalExam}
            subject={proposalExam ? subjectsById.get(proposalExam.subjectId) : undefined}
            confirming={confirming}
            error={confirmError}
            onConfirm={() => void confirm()}
            onDiscard={discard}
          />
        </section>
      )}

      {!!plannableExams.length && (
        <section className="mt-6">
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.1em] text-subtle">Para planificar</p>
          <div className="flex flex-col gap-2">
            {plannableExams.map((exam) => {
              const subject = subjectsById.get(exam.subjectId)
              const isCurrent = proposal?.examId === exam.id
              return (
                <Card key={exam.id} className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-ink">
                      {subject ? `${subject.name} — ` : ''}
                      {EXAM_TYPE_LABEL[exam.type]}
                    </p>
                    <p className="text-xs text-muted">
                      {formatDateLong(localDate(exam.startsAt))} · {formatDaysUntil(localDate(exam.startsAt))}
                    </p>
                    {exam.planStatus === 'desactualizado' && (
                      <p className="mt-0.5 text-xs text-warning">El plan quedó desactualizado: pedí uno nuevo.</p>
                    )}
                    {availability && !hasSlotBefore(availability, exam.startsAt) && (
                      <p className="mt-0.5 text-xs text-warning">No tenés horarios disponibles antes de este examen.</p>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => setDetailExam(exam)} aria-label="Editar o cancelar">
                    <Pencil size={14} />
                    Editar
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void requestProposal(exam.id)}
                    disabled={requestingId !== null || confirming}
                  >
                    <Sparkles size={14} />
                    {requestingId === exam.id
                      ? 'Pensando…'
                      : isCurrent
                        ? 'Pedir otra'
                        : exam.planStatus === 'desactualizado'
                          ? 'Replanificar'
                          : 'Pedir propuesta'}
                  </Button>
                  </div>
                </Card>
              )
            })}
          </div>
        </section>
      )}

      <section className="mt-6">
        {!sessions.length ? (
          !plannableExams.length &&
          !proposal && (
            <EmptyState
              title="No tenés sesiones reservadas"
              description="Cuando cargues un parcial o una entrega, va a aparecer acá para pedirle un plan al agente."
              action={
                <Link to="/calendario?nuevo=1">
                  <Button size="sm">
                    <CalendarPlus size={15} />
                    Cargar un examen
                  </Button>
                </Link>
              }
            />
          )
        ) : (
          <>
            <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.1em] text-subtle">Próximas sesiones</p>
            {upcoming.length ? (
              <SessionsByDay sessions={upcoming} subjectNameOf={subjectNameOf} />
            ) : (
              <p className="text-sm text-muted">No tenés sesiones por delante. Pedí un plan para tu próximo examen.</p>
            )}
          </>
        )}
      </section>

      {!!expired.length && (
        <CollapsibleSection
          title="Expiradas"
          count={expired.length}
          detail={expiredPending > 0 && <span className="text-danger">{expiredPending} sin marcar</span>}
          open={showExpired}
          onToggle={() => setShowExpired((v) => !v)}
        >
          <SessionsByDay sessions={expired} subjectNameOf={subjectNameOf} />
        </CollapsibleSection>
      )}

      {!!completed.length && (
        <CollapsibleSection title="Completadas" count={completed.length} open={showCompleted} onToggle={() => setShowCompleted((v) => !v)}>
          <SessionsByDay sessions={completed} subjectNameOf={subjectNameOf} />
        </CollapsibleSection>
      )}
      <ExamDetailSheet exam={detailExam} onClose={() => setDetailExam(null)} />
    </div>
  )
}

/** Sección plegable con la cantidad en el título. El botón tiene borde y fondo marcados para que se vea en claro y en oscuro. */
function CollapsibleSection({
  title,
  count,
  detail,
  open,
  onToggle,
  children,
}: {
  title: string
  count: number
  detail?: ReactNode
  open: boolean
  onToggle: () => void
  children: ReactNode
}) {
  return (
    <section className="mt-8">
      <button
        onClick={onToggle}
        className="flex w-full items-center justify-between rounded-xl border-2 border-subtle/60 bg-surface-raised px-4 py-3 text-left text-ink transition-colors hover:border-muted"
        aria-expanded={open}
      >
        <span className="text-sm">
          <span className="font-semibold">
            {title} ({count})
          </span>
          {detail && <span className="ml-2 text-xs"> · {detail}</span>}
        </span>
        {open ? <ChevronUp size={18} /> : <ChevronDown size={18} />}
      </button>
      {open && <div className="mt-4">{children}</div>}
    </section>
  )
}

/** Sesiones agrupadas por día, en el orden en que llegan. */
function SessionsByDay({ sessions, subjectNameOf }: { sessions: Session[]; subjectNameOf: (s: Session) => string | undefined }) {
  const days = new Map<string, Session[]>()
  for (const s of sessions) {
    const day = localDate(s.start)
    if (!days.has(day)) days.set(day, [])
    days.get(day)!.push(s)
  }
  const today = localDate(new Date())
  return (
    <div className="flex flex-col gap-5">
      {[...days.entries()].map(([date, items]) => (
        <div key={date}>
          <h2 className="mb-2 text-sm font-medium capitalize text-ink">
            {date === today ? `Hoy · ${formatDateLong(date)}` : formatDateLong(date)}
          </h2>
          <div className="flex flex-col gap-2">
            {items.map((session) => (
              <SessionCard key={session.id} session={session} subjectName={subjectNameOf(session)} />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

/** ¿Alguna franja semanal de la disponibilidad cae entre ahora y el comienzo del examen? */
function hasSlotBefore(availability: Availability, examStartsAt: string): boolean {
  const now = new Date()
  const examStart = new Date(examStartsAt)
  for (const day = new Date(now.getFullYear(), now.getMonth(), now.getDate()); day < examStart; day.setDate(day.getDate() + 1)) {
    for (const slot of availability.weeklySlots) {
      if (slot.weekday !== day.getDay()) continue
      const [sh, sm] = slot.startTime.split(':').map(Number)
      const [eh, em] = slot.endTime.split(':').map(Number)
      const start = new Date(day.getFullYear(), day.getMonth(), day.getDate(), sh, sm)
      const end = new Date(day.getFullYear(), day.getMonth(), day.getDate(), eh, em)
      if (end > now && start < examStart) return true
    }
  }
  return false
}
