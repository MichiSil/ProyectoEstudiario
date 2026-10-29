import clsx from 'clsx'
import { AlertTriangle, Bot, CheckCircle2, Clock, ListChecks, X } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { ApiError } from '@/api/client'
import { EXAM_TYPE_LABEL, type Exam, type PlanProposal, type Subject } from '@/api/types'
import { formatDateLong, formatMinutes, formatTimeRange, localDate } from '@/lib/format'

/**
 * Propuesta de plan (RF-08). Es sólo una sugerencia hasta que el estudiante la
 * confirma (RN-08). Si la confirmación incumple reglas, se marcan las sesiones
 * afectadas con el identificador de la regla.
 */
export function ProposalCard({
  proposal,
  exam,
  subject,
  confirming,
  error,
  onConfirm,
  onDiscard,
}: {
  proposal: PlanProposal
  exam: Exam | undefined
  subject: Subject | undefined
  confirming: boolean
  error: unknown
  onConfirm: () => void
  onDiscard: () => void
}) {
  const drafts = proposal.sessions ?? []
  const byAI = proposal.generatedBy === 'ia'
  const fellBack = proposal.warnings.includes('AI_FALLBACK')
  const violatedIndexes = new Set(
    error instanceof ApiError ? error.violations.filter((v) => v.sessionIndex >= 0).map((v) => v.sessionIndex) : [],
  )
  const totalMinutes = drafts.reduce((sum, d) => sum + d.durationMinutes, 0)

  return (
    <Card className="animate-fade-in border-accent/40 p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[11px] font-bold uppercase tracking-[0.1em] text-accent">Propuesta de plan</p>
          <h2 className="mt-1 text-lg">
            {subject ? `${subject.name} — ` : ''}
            {exam ? EXAM_TYPE_LABEL[exam.type] : 'Examen'}
          </h2>
          {exam && <p className="text-sm text-muted">{formatDateLong(localDate(exam.startsAt))}</p>}
        </div>
        <button onClick={onDiscard} aria-label="Descartar propuesta" className="rounded-full p-1.5 text-muted hover:bg-accent-soft">
          <X size={18} />
        </button>
      </div>

      <div className="mt-3 flex flex-wrap gap-2">
        <Badge tone={byAI ? 'accent' : 'neutral'}>
          {byAI ? <Bot size={12} /> : <ListChecks size={12} />}
          {byAI ? 'Armado por el agente de IA' : 'Armado por reglas fijas'}
        </Badge>
        <Badge tone="neutral">
          {drafts.length} sesión{drafts.length === 1 ? '' : 'es'} · {formatMinutes(totalMinutes)}
        </Badge>
      </div>

      {fellBack && (
        <p className="mt-3 flex items-start gap-2 rounded-xl bg-warning/10 px-3 py-2.5 text-sm text-ink">
          <AlertTriangle size={16} className="mt-0.5 shrink-0 text-warning" />
          El agente de IA no respondió a tiempo, así que armamos el plan con las reglas fijas de anticipación y
          dificultad. Podés confirmarlo igual.
        </p>
      )}

      {!drafts.length ? (
        <p className="mt-4 rounded-xl bg-surface-raised/70 px-4 py-6 text-center text-sm text-muted">
          No encontramos lugar en tu disponibilidad antes del examen. Sumá franjas horarias en Ajustes y volvé a pedir la
          propuesta.
        </p>
      ) : (
        <ol className="mt-4 flex flex-col gap-2">
          {drafts.map((d, i) => (
            <li
              key={`${d.start}-${i}`}
              className={clsx(
                'rounded-xl border px-4 py-3',
                violatedIndexes.has(i) ? 'border-danger/50 bg-danger/5' : 'border-border bg-paper',
              )}
            >
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <p className="text-sm font-medium text-ink">
                  <span className="mr-1.5 text-subtle">{i + 1}.</span>
                  {d.objective || d.topic}
                </p>
                <span className="flex items-center gap-1 text-xs capitalize text-subtle">
                  <Clock size={12} />
                  {formatDateLong(localDate(d.start))} · {formatTimeRange(d.start, d.end)}
                </span>
              </div>
              {d.objective && <p className="mt-0.5 text-xs font-medium text-muted">{d.topic}</p>}
              {d.reasoning && <p className="mt-1 text-xs text-muted">{d.reasoning}</p>}
            </li>
          ))}
        </ol>
      )}

      {!!error && (
        <div className="mt-4">
          <ApiErrorNotice error={error} sessionLabel={(i) => `Sesión ${i + 1}`} />
        </div>
      )}

      <div className="mt-5 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button variant="ghost" onClick={onDiscard} disabled={confirming}>
          Descartar
        </Button>
        <Button onClick={onConfirm} disabled={confirming || !drafts.length}>
          <CheckCircle2 size={16} />
          {confirming ? 'Confirmando…' : error ? 'Reintentar confirmación' : 'Confirmar plan'}
        </Button>
      </div>
      <p className="mt-2 text-right text-xs text-subtle">
        Al confirmar, las sesiones quedan reservadas en tu calendario. Nada se reserva hasta que confirmes.
      </p>
    </Card>
  )
}
