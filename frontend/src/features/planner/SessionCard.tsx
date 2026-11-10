import { useState } from 'react'
import clsx from 'clsx'
import { AlertCircle, CheckCircle2, Circle, Clock, PauseCircle, Undo2 } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { plannerApi } from '@/api/planner'
import type { Session, SessionStatus } from '@/api/types'
import { sessionPhase } from '@/lib/domain-ui'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { formatDateShort, formatMinutes, formatTimeRange, localDate, minutesBetween } from '@/lib/format'

/**
 * Sesión reservada (RF-10, RF-11). El círculo marca la sesión como completada
 * (o la vuelve a pendiente si se marcó por error). Una sesión que ya terminó
 * sin completarse se muestra en rojo, con las opciones de marcarla como hecha
 * o posponerla: posponer libera el horario y deja el plan desactualizado para
 * rehacerlo con ese tema (RN-49).
 */
export function SessionCard({ session, subjectName }: { session: Session; subjectName?: string }) {
  const { rememberSession, refresh } = usePlannerData()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const phase = sessionPhase(session)

  async function change(status: SessionStatus) {
    setSaving(true)
    setError(null)
    try {
      const updated = await plannerApi.setSessionStatus(session.id, status)
      rememberSession(updated)
      // Posponer deja el plan del examen desactualizado: hay que volver a leer los exámenes.
      if (status === 'pospuesta') await refresh()
    } catch (err) {
      setError(err)
    } finally {
      setSaving(false)
    }
  }

  const toggle = () => void change(phase === 'completada' ? 'pendiente' : 'completada')
  const canToggle = phase === 'completada' || phase === 'en-curso' || phase === 'no-hecha'

  return (
    <Card className={clsx('animate-fade-in', phase === 'no-hecha' && 'border-danger/40', phase === 'pospuesta' && 'opacity-70')}>
      <div className="flex items-start gap-3">
        <button
          onClick={toggle}
          disabled={!canToggle || saving}
          aria-label={phase === 'completada' ? 'Volver a pendiente' : 'Marcar como completada'}
          title={
            phase === 'futura'
              ? 'Vas a poder marcarla cuando empiece'
              : phase === 'completada'
                ? 'Completada: tocá para deshacer'
                : phase === 'pospuesta'
                  ? 'Pospuesta'
                  : 'Marcar como completada'
          }
          className={clsx(
            'mt-0.5 shrink-0 rounded-full transition-transform',
            canToggle && !saving && 'hover:scale-110 active:scale-95',
            !canToggle && 'cursor-default',
          )}
        >
          {phase === 'completada' ? (
            <CheckCircle2 size={22} className="text-success" />
          ) : phase === 'no-hecha' ? (
            <AlertCircle size={22} className="text-danger" />
          ) : phase === 'pospuesta' ? (
            <PauseCircle size={22} className="text-subtle" />
          ) : (
            <Circle size={22} className={phase === 'en-curso' ? 'text-accent' : 'text-border'} />
          )}
        </button>
        <div className="min-w-0 flex-1">
          <p className={clsx('text-sm font-medium text-ink', phase === 'completada' && 'text-muted line-through decoration-success/60')}>
            {subjectName ? `${subjectName} — ` : ''}
            {session.objective || session.topic}
          </p>
          {session.objective && <p className="mt-0.5 text-xs text-muted">{session.topic}</p>}
          <div className="mt-1.5 flex flex-wrap items-center gap-2">
            <Badge tone="neutral">{formatDateShort(localDate(session.start))}</Badge>
            <Badge tone="neutral">
              <Clock size={11} />
              {formatTimeRange(session.start, session.end)}
            </Badge>
            <Badge tone="neutral">{formatMinutes(minutesBetween(session.start, session.end))}</Badge>
            {phase === 'en-curso' && <Badge tone="accent">Ahora</Badge>}
            {phase === 'completada' && <Badge tone="success">Completada</Badge>}
            {phase === 'no-hecha' && <Badge tone="danger">No la marcaste</Badge>}
            {phase === 'pospuesta' && <Badge tone="neutral">Pospuesta</Badge>}
            {session.outdated && phase !== 'completada' && phase !== 'pospuesta' && <Badge tone="warning">Plan desactualizado</Badge>}
          </div>

          {phase === 'no-hecha' && (
            <div className="mt-3 flex flex-wrap gap-2">
              <Button size="sm" variant="secondary" onClick={() => void change('completada')} disabled={saving}>
                <CheckCircle2 size={14} />
                La hice
              </Button>
              <Button size="sm" variant="ghost" onClick={() => void change('pospuesta')} disabled={saving}>
                <PauseCircle size={14} />
                Posponer
              </Button>
            </div>
          )}
          {(phase === 'futura' || phase === 'en-curso') && (
            <button
              onClick={() => void change('pospuesta')}
              disabled={saving}
              className="mt-2 inline-flex items-center gap-1 text-xs text-subtle hover:text-ink"
            >
              <PauseCircle size={12} />
              No voy a poder: posponer
            </button>
          )}
          {phase === 'completada' && (
            <button
              onClick={toggle}
              disabled={saving}
              className="mt-2 inline-flex items-center gap-1 text-xs text-subtle hover:text-ink"
            >
              <Undo2 size={12} />
              Deshacer
            </button>
          )}
          {phase === 'pospuesta' && (
            <p className="mt-2 text-xs text-subtle">El horario quedó libre. Rehacé el plan del examen para volver a incluir este tema.</p>
          )}

          {!!error && (
            <div className="mt-3">
              <ApiErrorNotice error={error} />
            </div>
          )}
        </div>
      </div>
    </Card>
  )
}
