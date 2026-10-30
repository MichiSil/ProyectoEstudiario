import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { CalendarPlus, CheckCircle2, Settings, Sparkles } from 'lucide-react'
import { plannerApi } from '@/api/planner'
import { ApiError } from '@/api/client'
import { EXAM_TYPE_LABEL, type PlanProposal } from '@/api/types'
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
  const { exams, sessions, availability, subjectsById, examsById, refreshSessions } = usePlannerData()
  const [searchParams, setSearchParams] = useSearchParams()

  const [proposal, setProposal] = useState<PlanProposal | null>(null)
  const [requestingId, setRequestingId] = useState<string | null>(null)
  const [requestError, setRequestError] = useState<unknown>(null)
  const [confirming, setConfirming] = useState(false)
  const [confirmError, setConfirmError] = useState<unknown>(null)
  const [message, setMessage] = useState<string | null>(null)
  const idempotencyKeys = useRef(new Map<string, string>())

  const examsWithPlan = useMemo(() => new Set(sessions.map((s) => s.examId)), [sessions])
  const plannableExams = useMemo(
    () =>
      exams
        .filter((e) => e.status === 'programado' && new Date(e.startsAt) > new Date() && !examsWithPlan.has(e.id))
        .sort((a, b) => a.startsAt.localeCompare(b.startsAt)),
    [exams, examsWithPlan],
  )

  const today = localDate(new Date())
  const upcomingSessions = useMemo(
    () => sessions.filter((s) => s.status !== 'completada').sort((a, b) => a.start.localeCompare(b.start)),
    [sessions],
  )
  const grouped = useMemo(() => {
    const map = new Map<string, typeof upcomingSessions>()
    for (const s of upcomingSessions) {
      const day = localDate(s.start)
      if (!map.has(day)) map.set(day, [])
      map.get(day)!.push(s)
    }
    return map
  }, [upcomingSessions])

  async function requestProposal(examId: string) {
    setRequestingId(examId)
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
      setProposal(null)
      setMessage(`Listo: reservamos ${result.sessions.length} sesión${result.sessions.length === 1 ? '' : 'es'} en tu calendario.`)
      await refreshSessions()
    } catch (err) {
      setConfirmError(err)
      if (err instanceof ApiError && err.code === 'PLAN_ALREADY_CONFIRMED') await refreshSessions()
    } finally {
      setConfirming(false)
    }
  }

  function discard() {
    setProposal(null)
    setConfirmError(null)
  }

  const proposalExam = proposal ? examsById.get(proposal.examId) : undefined
  const missingData = requestError instanceof ApiError && requestError.status === 404

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
          />
          {missingData && (
            <Link to="/configuracion" className="mt-3 inline-block">
              <Button size="sm">
                <Settings size={14} />
                Cargar mi disponibilidad
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
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.1em] text-subtle">Todavía sin plan</p>
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
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void requestProposal(exam.id)}
                    disabled={requestingId !== null || confirming}
                  >
                    <Sparkles size={14} />
                    {requestingId === exam.id ? 'Pensando…' : isCurrent ? 'Pedir otra' : 'Pedir propuesta'}
                  </Button>
                </Card>
              )
            })}
          </div>
        </section>
      )}

      <section className="mt-6">
        {!upcomingSessions.length ? (
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
            <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.1em] text-subtle">Sesiones reservadas</p>
            <div className="flex flex-col gap-5">
              {[...grouped.entries()].map(([date, items]) => (
                <div key={date}>
                  <h2 className="mb-2 text-sm font-medium capitalize text-ink">
                    {date < today ? `${formatDateLong(date)} · ya pasó` : formatDateLong(date)}
                  </h2>
                  <div className="flex flex-col gap-2">
                    {items.map((session) => {
                      const exam = examsById.get(session.examId)
                      return (
                        <SessionCard
                          key={session.id}
                          session={session}
                          subjectName={exam ? subjectsById.get(exam.subjectId)?.name : undefined}
                        />
                      )
                    })}
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </section>
    </div>
  )
}
