import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { setApiUser } from '@/api/client'
import { readJson, removeKey, writeJson } from '@/lib/storage'

/**
 * Identidad del estudiante en el frontend.
 *
 * users-service todavía es un esqueleto (sólo GET /health), así que no hay
 * registro con contraseña ni JWT. Mientras tanto, el estudiante se identifica
 * con su mail y el cliente lo manda como X-User-Id, que es lo que planner-service
 * acepta hoy (handler.go). Cuando exista el login real, este provider pasa a
 * guardar el token y el resto de la app no cambia.
 */

export interface StudentIdentity {
  id: string
  displayName: string
}

interface SessionContextValue {
  user: StudentIdentity | null
  signIn: (identity: StudentIdentity) => void
  signOut: () => void
}

const STORAGE_KEY = 'estudiario-identity'

const SessionContext = createContext<SessionContextValue | null>(null)

export function SessionProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<StudentIdentity | null>(() => {
    const stored = readJson<StudentIdentity | null>(STORAGE_KEY, null)
    setApiUser(stored?.id ?? null)
    return stored
  })

  const signIn = useCallback((identity: StudentIdentity) => {
    const normalized = { id: identity.id.trim().toLowerCase(), displayName: identity.displayName.trim() }
    writeJson(STORAGE_KEY, normalized)
    setApiUser(normalized.id)
    setUser(normalized)
  }, [])

  const signOut = useCallback(() => {
    removeKey(STORAGE_KEY)
    setApiUser(null)
    setUser(null)
  }, [])

  const value = useMemo(() => ({ user, signIn, signOut }), [user, signIn, signOut])
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext)
  if (!ctx) throw new Error('useSession debe usarse dentro de SessionProvider')
  return ctx
}
