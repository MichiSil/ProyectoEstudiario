import { CalendarDays, Home, LayoutList, MessagesSquare, NotebookText, Settings, Timer } from 'lucide-react'
import type { ComponentType } from 'react'

export interface NavItem {
  to: string
  label: string
  icon: ComponentType<{ size?: number; className?: string }>
  /** En el celular no va en la barra de abajo (no entran 7): se llega desde la barra de arriba. */
  hideOnMobileBar?: boolean
}

export const NAV_ITEMS: NavItem[] = [
  { to: '/', label: 'Inicio', icon: Home },
  { to: '/calendario', label: 'Calendario', icon: CalendarDays },
  { to: '/materias', label: 'Materias', icon: NotebookText },
  { to: '/plan', label: 'Plan', icon: LayoutList },
  { to: '/foro', label: 'Foro', icon: MessagesSquare },
  { to: '/pomodoro', label: 'Pomodoro', icon: Timer },
  { to: '/configuracion', label: 'Ajustes', icon: Settings, hideOnMobileBar: true },
]
