import { useState, type FormEvent, type ReactNode } from 'react'
import clsx from 'clsx'
import { useSession } from '@/app/providers/SessionProvider'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { Button } from '@/components/ui/Button'
import { FieldGroup, Input, Label } from '@/components/ui/Field'

type Mode = 'login' | 'register'

/** Mínimo de la contraseña (RN-02); users-service lo vuelve a validar. */
const MIN_PASSWORD = 8

/**
 * Registro (RF-01) e inicio de sesión (RF-02) contra users-service. El mail es
 * único (RN-01) y el rol no se elige: lo asigna users-service (RF-03).
 */
export function AuthScreen() {
  const { signIn, register } = useSession()
  const [mode, setMode] = useState<Mode>('login')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const isRegister = mode === 'register'
  const passwordTooShort = isRegister && password.length > 0 && password.length < MIN_PASSWORD
  const canSubmit =
    !busy && !!email.trim() && !!password && (!isRegister || (!!name.trim() && password.length >= MIN_PASSWORD))

  function switchMode(next: Mode) {
    setMode(next)
    setError(null)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!canSubmit) return
    setBusy(true)
    setError(null)
    try {
      if (isRegister) await register(name, email, password)
      else await signIn(email, password)
    } catch (err) {
      setError(err)
      setBusy(false)
    }
  }

  return (
    <AuthShell>
      <div className="mb-5 grid grid-cols-2 gap-1 rounded-xl bg-surface-raised p-1" role="tablist">
        <ModeTab active={!isRegister} onClick={() => switchMode('login')}>
          Iniciar sesión
        </ModeTab>
        <ModeTab active={isRegister} onClick={() => switchMode('register')}>
          Crear cuenta
        </ModeTab>
      </div>

      <form onSubmit={handleSubmit}>
        {isRegister && (
          <FieldGroup>
            <Label>Nombre</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Cómo querés que te llamemos"
              autoComplete="name"
              required
            />
          </FieldGroup>
        )}
        <FieldGroup>
          <Label>Mail</Label>
          <Input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="vos@ejemplo.com"
            autoComplete="email"
            required
          />
        </FieldGroup>
        <FieldGroup>
          <Label>Contraseña</Label>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={isRegister ? `Mínimo ${MIN_PASSWORD} caracteres` : ''}
            autoComplete={isRegister ? 'new-password' : 'current-password'}
            minLength={isRegister ? MIN_PASSWORD : undefined}
            required
          />
          {passwordTooShort && (
            <p className="mt-1.5 text-xs text-danger">La contraseña tiene que tener al menos {MIN_PASSWORD} caracteres.</p>
          )}
        </FieldGroup>

        {!!error && (
          <div className="mb-4">
            <ApiErrorNotice error={error} />
          </div>
        )}

        <Button type="submit" className="w-full" disabled={!canSubmit}>
          {busy ? 'Un momento…' : isRegister ? 'Crear cuenta' : 'Ingresar'}
        </Button>
      </form>
    </AuthShell>
  )
}

function ModeTab({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={clsx(
        'rounded-lg px-3 py-2 text-sm font-medium transition-colors',
        active ? 'bg-surface text-ink shadow-[var(--shadow-sm)]' : 'text-muted hover:text-ink',
      )}
    >
      {children}
    </button>
  )
}

function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-full flex-col items-center justify-center bg-paper px-6 pt-[max(env(safe-area-inset-top),2.5rem)] pb-[max(env(safe-area-inset-bottom),2.5rem)]">
      <div className="animate-fade-in mb-8 flex flex-col items-center gap-3">
        <img src="/estudiarioimg1.png" alt="" className="h-20 w-20 object-contain" />
        <h1 className="text-2xl font-bold tracking-tight">Estudiario</h1>
        <p className="max-w-[22rem] text-center text-sm text-muted">
          Un espacio propio para organizar tu semana de estudio. Tus materias y tu plan son privados: solo vos podés
          verlos.
        </p>
      </div>
      <div className="w-full max-w-sm rounded-xl border border-border bg-surface p-6 shadow-[var(--shadow-sm)]">
        {children}
      </div>
    </div>
  )
}
