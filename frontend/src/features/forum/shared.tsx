import clsx from 'clsx'
import { FileText, Star } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'
import type { Summary, SummaryStatus } from '@/api/types'
import { formatDateShort, localDate } from '@/lib/format'

export const STATUS_LABEL: Record<SummaryStatus, string> = {
  pendiente: 'En revisión',
  aprobado: 'Publicado',
  rechazado: 'Rechazado',
  eliminado: 'Eliminado',
}

const STATUS_TONE: Record<SummaryStatus, 'warning' | 'success' | 'danger' | 'neutral'> = {
  pendiente: 'warning',
  aprobado: 'success',
  rechazado: 'danger',
  eliminado: 'neutral',
}

export function StatusBadge({ status }: { status: SummaryStatus }) {
  return <Badge tone={STATUS_TONE[status]}>{STATUS_LABEL[status]}</Badge>
}

export function formatSize(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1).replace('.', ',')} MB`
}

/** Promedio de 1 a 5 con la cantidad de calificaciones (RN-31). */
export function RatingSummary({ rating }: { rating: Summary['rating'] }) {
  if (!rating.count || rating.average === null) return <span className="text-xs text-subtle">Sin calificaciones</span>
  return (
    <span className="flex items-center gap-1 text-xs text-muted">
      <Star size={13} className="fill-warning text-warning" />
      <span className="font-semibold text-ink">{rating.average.toFixed(1).replace('.', ',')}</span>
      <span>
        ({rating.count} {rating.count === 1 ? 'calificación' : 'calificaciones'})
      </span>
    </span>
  )
}

/** Cinco estrellas para elegir un puntaje (RF-22). */
export function StarPicker({ value, onChange, disabled }: { value: number; onChange: (score: number) => void; disabled?: boolean }) {
  return (
    <div className="flex gap-1" role="radiogroup" aria-label="Calificación">
      {[1, 2, 3, 4, 5].map((score) => (
        <button
          key={score}
          type="button"
          role="radio"
          aria-checked={value === score}
          aria-label={`${score} de 5`}
          disabled={disabled}
          onClick={() => onChange(score)}
          className="rounded-lg p-1 transition-transform hover:scale-110 disabled:opacity-50"
        >
          <Star size={26} className={clsx(score <= value ? 'fill-warning text-warning' : 'text-border')} />
        </button>
      ))}
    </div>
  )
}

/** Fila de un resumen en las listas del foro. */
export function SummaryRow({
  summary,
  onClick,
  showStatus,
  children,
}: {
  summary: Summary
  onClick?: () => void
  showStatus?: boolean
  children?: React.ReactNode
}) {
  const date = summary.publishedAt ?? summary.createdAt
  return (
    <div className="rounded-xl border border-border bg-surface p-4 shadow-[var(--shadow-sm)]">
      <button type="button" onClick={onClick} className="flex w-full items-start gap-3 text-left" disabled={!onClick}>
        <span className="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
          <FileText size={18} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-semibold text-ink">{summary.title}</span>
          <span className="mt-0.5 block truncate text-xs text-muted">
            {summary.category} · {summary.author.name} · {formatDateShort(localDate(date))}
          </span>
          <span className="mt-1.5 flex flex-wrap items-center gap-2">
            {showStatus && <StatusBadge status={summary.status} />}
            {summary.status === 'aprobado' && <RatingSummary rating={summary.rating} />}
          </span>
        </span>
      </button>
      {summary.status === 'rechazado' && summary.rejectionReason && (
        <p className="mt-3 rounded-lg bg-danger/10 px-3 py-2 text-xs text-ink">
          <span className="font-semibold">Motivo del rechazo: </span>
          {summary.rejectionReason}
        </p>
      )}
      {children}
    </div>
  )
}
