import { useEffect, useState, type ReactNode } from 'react'
import { useSession } from '@/app/providers/SessionProvider'
import { PlannerDataProvider } from '@/app/providers/PlannerDataProvider'
import { BrandSplash } from '@/components/BrandSplash'
import { AuthScreen } from './AuthScreen'

const SPLASH_FLAG = 'estudiario-splash-shown'

function splashAlreadyShown(): boolean {
  try {
    return !!sessionStorage.getItem(SPLASH_FLAG)
  } catch {
    return true
  }
}

/**
 * Sin identidad muestra el ingreso. Con identidad, un momento de marca breve
 * una vez por sesión de navegador y después la app, con los datos del
 * estudiante cargados desde planner-service.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const { user } = useSession()
  const [showEntrySplash, setShowEntrySplash] = useState(() => !!user && !splashAlreadyShown())

  useEffect(() => {
    if (!showEntrySplash) return
    try {
      sessionStorage.setItem(SPLASH_FLAG, '1')
    } catch {
      // sin sessionStorage el splash simplemente vuelve a aparecer
    }
    const timeout = setTimeout(() => setShowEntrySplash(false), 850)
    return () => clearTimeout(timeout)
  }, [showEntrySplash])

  if (!user) return <AuthScreen />
  if (showEntrySplash) return <BrandSplash tagline="Organizá tu semana académica" />

  return <PlannerDataProvider key={user.id}>{children}</PlannerDataProvider>
}
