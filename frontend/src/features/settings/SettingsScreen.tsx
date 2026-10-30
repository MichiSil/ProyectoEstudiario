import clsx from 'clsx'
import { Link } from 'react-router-dom'
import { LogOut, ShieldCheck, UserRound } from 'lucide-react'
import { PageHeader } from '@/components/PageHeader'
import { Card } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { useSession } from '@/app/providers/SessionProvider'
import { useTheme } from '@/app/providers/ThemeProvider'
import { AvailabilitySection } from './AvailabilitySection'

export function SettingsScreen() {
  const { user } = useSession()
  return (
    <div className="mx-auto max-w-2xl px-4 py-6">
      <PageHeader title="Configuración" subtitle="Tu perfil, disponibilidad y preferencias." />

      <div className="mt-5 flex flex-col gap-4">
        <ProfileSection />
        {user?.role === 'admin' && <AdminSection />}
        <AvailabilitySection />
        <AppearanceSection />
      </div>
    </div>
  )
}

function AdminSection() {
  return (
    <Card className="border-accent/30">
      <div className="flex items-start gap-3">
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-accent-soft text-accent-ink">
          <ShieldCheck size={20} />
        </span>
        <div className="min-w-0">
          <h3 className="font-medium text-ink">Panel de administración</h3>
          <p className="mt-0.5 text-sm text-muted">Revisá los resúmenes que subieron al foro: aprobalos, rechazalos con un motivo o eliminalos.</p>
        </div>
      </div>
      <Link to="/foro?tab=moderacion" className="mt-4 block">
        <Button className="w-full sm:w-auto">
          <ShieldCheck size={16} />
          Abrir el panel de admin
        </Button>
      </Link>
    </Card>
  )
}

function ProfileSection() {
  const { user, signOut } = useSession()
  return (
    <Card>
      <h3 className="font-medium text-ink">Perfil de estudiante</h3>
      <div className="mt-3 flex items-center gap-4">
        <span className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent-ink">
          <UserRound size={24} />
        </span>
        <div className="min-w-0 text-sm">
          <p className="flex items-center gap-2 truncate font-medium text-ink">
            {user?.name}
            {user?.role === 'admin' && (
              <span className="rounded-full bg-accent-soft px-2 py-0.5 text-[11px] font-semibold text-accent-ink">Admin</span>
            )}
          </p>
          <p className="truncate text-muted">{user?.email}</p>
        </div>
      </div>
      <Button variant="ghost" className="mt-4" onClick={signOut}>
        <LogOut size={16} />
        Cerrar sesión
      </Button>
    </Card>
  )
}

const THEME_OPTIONS = [
  { value: 'system', label: 'Automático' },
  { value: 'light', label: 'Claro' },
  { value: 'dark', label: 'Oscuro' },
] as const

function AppearanceSection() {
  const { preference, setPreference } = useTheme()
  return (
    <Card>
      <h3 className="font-medium text-ink">Apariencia</h3>
      <div className="mt-3 flex gap-2">
        {THEME_OPTIONS.map((o) => (
          <button
            key={o.value}
            onClick={() => setPreference(o.value)}
            className={clsx(
              'flex-1 rounded-xl border px-3 py-2 text-sm font-medium',
              preference === o.value ? 'border-accent bg-accent-soft text-accent-ink' : 'border-border text-muted hover:bg-surface-raised',
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
    </Card>
  )
}
