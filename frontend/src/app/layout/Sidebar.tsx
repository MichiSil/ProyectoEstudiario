import { Link, NavLink } from 'react-router-dom'
import clsx from 'clsx'
import { UserRound } from 'lucide-react'
import { NAV_ITEMS } from '@/app/nav'
import { useSession } from '@/app/providers/SessionProvider'

export function Sidebar() {
  const { user } = useSession()

  return (
    <aside className="hidden w-64 shrink-0 flex-col overflow-y-auto border-r border-border/70 bg-surface px-6 pt-[max(env(safe-area-inset-top),3rem)] pb-8 sm:flex">
      <Link to="/" className="mb-12 flex items-center gap-3">
        <img src="/logo-mark.png" alt="" className="h-9 w-9 object-contain" />
        <span className="text-lg font-extrabold tracking-tight text-ink">Estudiario</span>
      </Link>

      <ul className="flex flex-col gap-0.5">
        {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
          <li key={to}>
            <NavLink
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                clsx(
                  'flex items-center gap-3 rounded-lg px-3 py-2.5 text-[14.5px] font-medium',
                  isActive
                    ? 'bg-accent text-white shadow-[var(--shadow-sm)]'
                    : 'text-muted hover:bg-surface-raised hover:text-ink',
                )
              }
            >
              <Icon size={18} />
              {label}
            </NavLink>
          </li>
        ))}
      </ul>

      <Link to="/configuracion" className="mt-auto flex items-center gap-2.5 border-t border-border/70 pt-4">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-surface-raised text-muted">
          <UserRound size={16} />
        </span>
        <div className="min-w-0">
          <p className="truncate text-[13px] font-medium text-ink">{user?.displayName || user?.id || 'Tu cuenta'}</p>
          <p className="truncate text-[11px] text-subtle">{user?.id}</p>
        </div>
      </Link>
    </aside>
  )
}
