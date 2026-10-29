import { useState, type FormEvent, type ReactNode } from 'react'
import { Info } from 'lucide-react'
import { useSession } from '@/app/providers/SessionProvider'
import { Button } from '@/components/ui/Button'
import { FieldGroup, Input, Label } from '@/components/ui/Field'

/**
 * Ingreso del estudiante. Todavía no hay contraseña porque users-service es un
 * esqueleto (RF-01 y RF-02 llegan con él): el mail identifica al estudiante
 * ante planner-service y sus datos quedan separados de los de los demás (RN-03).
 */
export function AuthScreen() {
  const { signIn } = useSession()
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!email.trim()) return
    signIn({ id: email, displayName: displayName || email.split('@')[0] })
  }

  return (
    <AuthShell>
      <form onSubmit={handleSubmit}>
        <FieldGroup>
          <Label>Nombre</Label>
          <Input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Cómo querés que te llamemos"
          />
        </FieldGroup>
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

        <Button type="submit" className="w-full" disabled={!email.trim()}>
          Ingresar
        </Button>
      </form>

      <p className="mt-5 flex gap-2 rounded-xl bg-surface-raised/70 px-3 py-2.5 text-xs text-muted">
        <Info size={14} className="mt-0.5 shrink-0 text-subtle" />
        El registro con contraseña se habilita cuando esté operativo el servicio de usuarios. Por ahora, tu mail
        identifica tus materias, exámenes y plan.
      </p>
    </AuthShell>
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
