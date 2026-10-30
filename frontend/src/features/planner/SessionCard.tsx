import { CheckCircle2, Circle, Clock } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import type { Session } from '@/api/types'
import { SESSION_STATUS_LABEL } from '@/lib/domain-ui'
import { formatDateShort, formatMinutes, formatTimeRange, localDate, minutesBetween } from '@/lib/format'

/** Sesión reservada en el calendario. */
export function SessionCard({ session, subjectName }: { session: Session; subjectName?: string }) {
  return (
    <Card className="animate-fade-in">
      <div className="flex items-start gap-3">
        <span className="mt-0.5 shrink-0 text-subtle">
          {session.status === 'completada' ? <CheckCircle2 size={20} className="text-success" /> : <Circle size={20} />}
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-ink">
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
            {session.outdated && session.status !== 'completada' && <Badge tone="warning">Desactualizada</Badge>}
            {session.status !== 'pendiente' && (
              <Badge tone={session.status === 'completada' ? 'success' : 'warning'}>{SESSION_STATUS_LABEL[session.status]}</Badge>
            )}
          </div>
        </div>
      </div>
    </Card>
  )
}
