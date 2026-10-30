import { useEffect, useState } from 'react'
import { CheckCircle2, Download, Trash2 } from 'lucide-react'
import { Sheet } from '@/components/ui/Sheet'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { forumApi } from '@/api/forum'
import type { Summary } from '@/api/types'
import { formatDateLong, localDate } from '@/lib/format'
import { formatSize, RatingSummary, StarPicker, StatusBadge } from './shared'

/**
 * Detalle de un resumen (RF-21): descripción, descarga del PDF y calificación
 * de 1 a 5 (RF-22). Un admin además puede eliminarlo (RF-18).
 */
export function SummaryDetailSheet({
  summary,
  isAdmin,
  onClose,
  onChanged,
  onDeleted,
}: {
  summary: Summary | null
  isAdmin: boolean
  onClose: () => void
  onChanged: (s: Summary) => void
  onDeleted: (id: string) => void
}) {
  const [score, setScore] = useState(0)
  const [busy, setBusy] = useState<'rate' | 'download' | 'delete' | null>(null)
  const [rated, setRated] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    setScore(0)
    setRated(false)
    setConfirmDelete(false)
    setError(null)
  }, [summary?.id])

  if (!summary) return null
  const current = summary

  async function run(kind: 'rate' | 'download' | 'delete', action: () => Promise<void>) {
    setBusy(kind)
    setError(null)
    try {
      await action()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(null)
    }
  }

  function rate(value: number) {
    setScore(value)
    void run('rate', async () => {
      onChanged(await forumApi.rate(current.id, value))
      setRated(true)
    })
  }

  return (
    <Sheet open onClose={onClose} title="Resumen">
      <h3 className="text-lg font-semibold text-ink">{current.title}</h3>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <Badge tone="accent">{current.category}</Badge>
        {current.status !== 'aprobado' && <StatusBadge status={current.status} />}
        {current.status === 'aprobado' && <RatingSummary rating={current.rating} />}
      </div>
      <p className="mt-3 text-xs text-muted">
        Subido por <span className="font-medium text-ink">{current.author.name}</span> el{' '}
        {formatDateLong(localDate(current.publishedAt ?? current.createdAt))} · PDF de {formatSize(current.file.sizeBytes)}
      </p>

      {current.description && <p className="mt-4 whitespace-pre-line text-sm text-ink">{current.description}</p>}
      {!!current.tags.length && (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {current.tags.map((t) => (
            <Badge key={t} tone="neutral">
              #{t}
            </Badge>
          ))}
        </div>
      )}

      <Button className="mt-5 w-full" onClick={() => void run('download', () => forumApi.download(current))} disabled={busy !== null}>
        <Download size={16} />
        {busy === 'download' ? 'Descargando…' : 'Descargar PDF'}
      </Button>

      {current.status === 'aprobado' && (
        <div className="mt-5 rounded-xl bg-paper p-4">
          <p className="text-sm font-medium text-ink">¿Te sirvió? Calificalo</p>
          <p className="mt-0.5 text-xs text-muted">Si ya lo calificaste, tu puntaje nuevo reemplaza al anterior.</p>
          <div className="mt-2 flex items-center gap-3">
            <StarPicker value={score} onChange={rate} disabled={busy === 'rate'} />
            {rated && (
              <span className="animate-fade-in flex items-center gap-1 text-xs text-success">
                <CheckCircle2 size={14} />
                ¡Gracias!
              </span>
            )}
          </div>
        </div>
      )}

      {!!error && (
        <div className="mt-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      {isAdmin &&
        (confirmDelete ? (
          <div className="mt-5 rounded-xl bg-danger/10 p-3">
            <p className="mb-3 text-sm text-ink">¿Eliminar este resumen? Deja de aparecer en el foro para todos.</p>
            <div className="grid grid-cols-2 gap-2">
              <Button variant="secondary" onClick={() => setConfirmDelete(false)} disabled={busy !== null}>
                Volver
              </Button>
              <Button
                variant="danger"
                disabled={busy !== null}
                onClick={() =>
                  void run('delete', async () => {
                    await forumApi.remove(current.id)
                    onDeleted(current.id)
                    onClose()
                  })
                }
              >
                {busy === 'delete' ? 'Eliminando…' : 'Sí, eliminar'}
              </Button>
            </div>
          </div>
        ) : (
          <Button variant="ghost" className="mt-3 w-full text-danger!" onClick={() => setConfirmDelete(true)}>
            <Trash2 size={16} />
            Eliminar (administración)
          </Button>
        ))}
    </Sheet>
  )
}
