import { useCallback, useEffect, useState } from 'react'
import { Check, Download, ShieldCheck, Trash2, X } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Textarea } from '@/components/ui/Field'
import { EmptyState } from '@/components/ui/EmptyState'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { ApiError } from '@/api/client'
import { forumApi } from '@/api/forum'
import type { Summary } from '@/api/types'
import { SummaryRow } from './shared'

type Action = { id: string; kind: 'approve' | 'reject' | 'delete' | 'download' }

/**
 * Panel de moderación (sólo admin): pendientes de la más vieja a la más nueva
 * (RF-16), aprobar o rechazar con motivo (RF-17, RN-22) y eliminar (RF-18).
 * Si otro admin ya la moderó (RN-23), se avisa y se actualiza la lista.
 */
export function ModerationPanel({ onCountChange }: { onCountChange: (n: number) => void }) {
  const [items, setItems] = useState<Summary[] | null>(null)
  const [busy, setBusy] = useState<Action | null>(null)
  const [rejecting, setRejecting] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<{ id: string | null; err: unknown } | null>(null)
  const [done, setDone] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const page = await forumApi.pending()
      setItems(page.items)
      onCountChange(page.total)
    } catch (err) {
      setItems([])
      setError({ id: null, err })
    }
  }, [onCountChange])

  useEffect(() => {
    void load()
  }, [load])

  async function run(action: Action, fn: () => Promise<unknown>, message?: string) {
    setBusy(action)
    setError(null)
    setDone(null)
    try {
      await fn()
      if (message) {
        setDone(message)
        setRejecting(null)
        setConfirmDelete(null)
        setReason('')
        await load()
      }
    } catch (err) {
      setError({ id: action.id, err })
      if (err instanceof ApiError && (err.code === 'ALREADY_MODERATED' || err.code === 'NOT_FOUND')) await load()
    } finally {
      setBusy(null)
    }
  }

  if (items === null) return <p className="py-8 text-center text-sm text-muted">Cargando pendientes…</p>

  return (
    <div className="flex flex-col gap-3">
      {done && <p className="animate-fade-in rounded-xl bg-success/10 px-4 py-3 text-sm text-ink">{done}</p>}
      {error?.id === null && <ApiErrorNotice error={error.err} />}

      {!items.length ? (
        <EmptyState
          icon={<ShieldCheck size={28} />}
          title="No hay resúmenes para revisar"
          description="Cuando alguien suba uno, aparece acá para que lo apruebes o lo rechaces."
        />
      ) : (
        <>
          <p className="text-xs text-muted">Del más viejo al más nuevo. Revisá el PDF antes de decidir.</p>
          {items.map((s) => {
            const isBusy = busy?.id === s.id
            return (
              <SummaryRow key={s.id} summary={s}>
                {s.description && <p className="mt-3 line-clamp-3 text-xs text-muted">{s.description}</p>}
                {s.source === 'externo' && (
                  <p className="mt-2 text-xs text-subtle">Registrado por un sistema externo (Grupo 4).</p>
                )}

                {rejecting === s.id ? (
                  <div className="mt-3">
                    <Textarea
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                      placeholder="Motivo del rechazo (lo ve quien lo subió)"
                      className="min-h-20"
                      autoFocus
                    />
                    <div className="mt-2 grid grid-cols-2 gap-2">
                      <Button variant="secondary" size="sm" onClick={() => setRejecting(null)} disabled={isBusy}>
                        Volver
                      </Button>
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={isBusy || !reason.trim()}
                        onClick={() =>
                          void run({ id: s.id, kind: 'reject' }, () => forumApi.reject(s.id, reason.trim()), `Rechazaste "${s.title}".`)
                        }
                      >
                        {isBusy ? 'Rechazando…' : 'Rechazar'}
                      </Button>
                    </div>
                  </div>
                ) : confirmDelete === s.id ? (
                  <div className="mt-3 rounded-lg bg-danger/10 p-3">
                    <p className="mb-2 text-xs text-ink">¿Eliminarlo sin moderar? No se puede deshacer.</p>
                    <div className="grid grid-cols-2 gap-2">
                      <Button variant="secondary" size="sm" onClick={() => setConfirmDelete(null)} disabled={isBusy}>
                        Volver
                      </Button>
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={isBusy}
                        onClick={() => void run({ id: s.id, kind: 'delete' }, () => forumApi.remove(s.id), `Eliminaste "${s.title}".`)}
                      >
                        Sí, eliminar
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                    <Button
                      size="sm"
                      disabled={isBusy}
                      onClick={() => void run({ id: s.id, kind: 'approve' }, () => forumApi.approve(s.id), `Publicaste "${s.title}".`)}
                    >
                      <Check size={15} />
                      {isBusy && busy?.kind === 'approve' ? 'Aprobando…' : 'Aprobar'}
                    </Button>
                    <Button variant="secondary" size="sm" disabled={isBusy} onClick={() => setRejecting(s.id)}>
                      <X size={15} />
                      Rechazar
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={isBusy}
                      onClick={() => void run({ id: s.id, kind: 'download' }, () => forumApi.download(s))}
                    >
                      <Download size={15} />
                      Ver PDF
                    </Button>
                    <Button variant="ghost" size="sm" className="text-danger!" disabled={isBusy} onClick={() => setConfirmDelete(s.id)}>
                      <Trash2 size={15} />
                      Eliminar
                    </Button>
                  </div>
                )}
                {error?.id === s.id && (
                  <div className="mt-3">
                    <ApiErrorNotice error={error.err} />
                  </div>
                )}
              </SummaryRow>
            )
          })}
        </>
      )}
    </div>
  )
}
