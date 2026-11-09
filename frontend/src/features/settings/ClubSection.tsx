import { useEffect, useState } from 'react'
import { format } from 'date-fns'
import { es } from 'date-fns/locale'
import { AlertCircle, Gift, Mail, RefreshCw } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { usersApi } from '@/api/users'
import type { ClubStatus } from '@/api/types'

/** Puntos que suma cada resumen aprobado (RF-28). */
const POINTS_PER_SUMMARY = 50

/**
 * Club de Beneficios de Metalfitness (Grupo 1, RF-29, ADR-015). El frontend
 * sólo habla con users-service por el gateway: nunca con el Club ni con su
 * API key. Si el Club no responde, se muestra lo que hay con un aviso.
 */
export function ClubSection() {
  const [club, setClub] = useState<ClubStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [linking, setLinking] = useState(false)
  const [error, setError] = useState<unknown>(null)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      setClub(await usersApi.club())
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  async function handleLink() {
    setLinking(true)
    setError(null)
    try {
      setClub(await usersApi.linkClub())
    } catch (err) {
      setError(err)
    } finally {
      setLinking(false)
    }
  }

  return (
    <Card>
      <div className="flex items-start gap-3">
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
          <Gift size={20} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-medium text-ink">Club de Beneficios Metalfitness</h3>
            {club?.status === 'vinculada' && <Badge tone="success">Vinculada</Badge>}
            {club?.status === 'pendiente' && <Badge tone="warning">Pendiente</Badge>}
          </div>
          <p className="mt-0.5 text-sm text-muted">
            Ganás {POINTS_PER_SUMMARY} puntos en el club del gimnasio cada vez que se aprueba un resumen tuyo en el foro.
          </p>
        </div>
      </div>

      {loading && !club && <p className="mt-4 text-sm text-subtle">Cargando tus puntos…</p>}

      {club?.warning === 'CLUB_UNAVAILABLE' && (
        <div className="mt-4 flex items-start gap-2 rounded-xl bg-warning/10 px-3 py-2.5 text-sm text-warning">
          <AlertCircle size={16} className="mt-0.5 shrink-0" />
          <span>No pudimos consultar tus puntos ahora. Tus puntos no se pierden: probá de nuevo en un rato.</span>
        </div>
      )}

      {club?.status === 'sin_vincular' && (
        <div className="mt-4">
          <p className="text-sm text-muted">
            Vinculá tu cuenta con tu mail de Estudiario. Si no tenés cuenta en el club, se crea una y te llega un mail para
            activarla.
          </p>
          <Button className="mt-3 w-full sm:w-auto" onClick={() => void handleLink()} disabled={linking}>
            <Gift size={16} />
            {linking ? 'Vinculando…' : 'Vincular mi cuenta del club'}
          </Button>
        </div>
      )}

      {club?.status === 'pendiente' && (
        <div className="mt-4 flex items-start gap-2 text-sm text-muted">
          <Mail size={16} className="mt-0.5 shrink-0" />
          <span>Revisá tu mail y confirmá la vinculación desde el mensaje del club. Después actualizá esta sección.</span>
        </div>
      )}

      {club?.status === 'vinculada' && club.balance !== null && (
        <div className="mt-4">
          <p className="text-3xl font-semibold text-ink">
            {club.balance} <span className="text-base font-normal text-muted">puntos</span>
          </p>
          {club.movements.length > 0 && (
            <ul className="mt-3 divide-y divide-border text-sm">
              {club.movements.map((m) => (
                <li key={m.id} className="flex items-center justify-between gap-3 py-2">
                  <span className="min-w-0 truncate text-muted">
                    {m.reason} · {format(new Date(m.occurredAt), "d 'de' MMM", { locale: es })}
                  </span>
                  <span className="shrink-0 font-medium text-success">+{m.points}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {!!club && club.pendingPoints > 0 && (
        <p className="mt-3 text-sm text-subtle">
          {club.pendingPoints} puntos ganados todavía no se acreditaron
          {club.status === 'vinculada' ? ': se acreditan solos en unos minutos.' : ': se acreditan cuando vincules tu cuenta.'}
        </p>
      )}

      {!!error && (
        <div className="mt-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      {!!club && club.status !== 'sin_vincular' && (
        <Button variant="ghost" size="sm" className="mt-3" onClick={() => void load()} disabled={loading}>
          <RefreshCw size={14} />
          Actualizar
        </Button>
      )}
    </Card>
  )
}
