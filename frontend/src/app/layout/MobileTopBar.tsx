import { Link, NavLink } from 'react-router-dom'
import clsx from 'clsx'
import { Settings } from 'lucide-react'

export function MobileTopBar() {
  return (
    <div className="flex items-center justify-between px-5 pt-[max(env(safe-area-inset-top),1.75rem)] pb-3 sm:hidden">
      <Link to="/" className="flex items-center gap-2.5">
        <img src="/logo-mark.png" alt="" className="h-9 w-9 object-contain" />
        <span className="text-[17px] font-extrabold tracking-tight text-ink">Estudiario</span>
      </Link>
      {/* Ajustes no entra en la barra de abajo: queda acá arriba. */}
      <NavLink
        to="/configuracion"
        aria-label="Ajustes"
        className={({ isActive }) =>
          clsx('flex h-10 w-10 items-center justify-center rounded-xl', isActive ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-surface-raised')
        }
      >
        <Settings size={21} />
      </NavLink>
    </div>
  )
}
