import { useState, type FormEvent } from 'react'
import { CheckCircle2 } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input, Label } from '@/components/ui/Field'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { usersApi } from '@/api/users'

/** Mismo mínimo que valida users-service (RN-02). */
const MIN_PASSWORD = 8

/** Cambiar la contraseña (RF-30): por ejemplo, la que recibieron por mail los autores de la agencia. */
export function PasswordSection() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [repeat, setRepeat] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [saved, setSaved] = useState(false)

  const mismatch = repeat !== '' && next !== repeat
  const invalid = !current || next.length < MIN_PASSWORD || next !== repeat

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError(null)
    setSaved(false)
    try {
      await usersApi.changePassword({ currentPassword: current, newPassword: next })
      setCurrent('')
      setNext('')
      setRepeat('')
      setSaved(true)
    } catch (err) {
      setError(err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card>
      <h3 className="font-medium text-ink">Cambiar contraseña</h3>
      <form className="mt-3 flex flex-col gap-3" onSubmit={(e) => void handleSubmit(e)}>
        <div>
          <Label>Contraseña actual</Label>
          <Input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="min-w-0">
            <Label>Nueva contraseña</Label>
            <Input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
          </div>
          <div className="min-w-0">
            <Label>Repetila</Label>
            <Input type="password" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} />
          </div>
        </div>
        {next !== '' && next.length < MIN_PASSWORD && (
          <p className="text-xs text-subtle">Tiene que tener al menos {MIN_PASSWORD} caracteres.</p>
        )}
        {mismatch && <p className="text-xs text-danger">Las contraseñas nuevas no coinciden.</p>}

        {!!error && <ApiErrorNotice error={error} />}

        <div className="flex items-center gap-3">
          <Button type="submit" disabled={saving || invalid}>
            {saving ? 'Guardando…' : 'Cambiar contraseña'}
          </Button>
          {saved && (
            <span className="animate-fade-in flex items-center gap-1.5 text-sm text-success">
              <CheckCircle2 size={15} />
              Contraseña cambiada
            </span>
          )}
        </div>
      </form>
    </Card>
  )
}
