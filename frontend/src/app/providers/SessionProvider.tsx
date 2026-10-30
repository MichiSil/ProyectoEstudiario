import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { onUnauthorized, setApiToken } from '@/api/client'
import type { User } from '@/api/types'
import { usersApi } from '@/api/users'
import { readJson, removeKey, writeJson } from '@/lib/storage'

/**
 * Sesión del estudiante (RF-01, RF-02).
 *
 * El JWT que devuelve users-service al iniciar sesión se guarda en el
 * navegador y el cliente lo manda como Authorization: Bearer en cada pedido.
 * La sesión se cierra al cerrar sesión, cuando vence el token o cuando el
 * gateway responde 401.
 */

interface StoredSession {
  token: string
  expiresAt: string
  user: User
}

interface SessionContextValue {
  user: User | null
  signIn: (email: string, password: string) => Promise<void>
  register: (name: string, email: string, password: string) => Promise<void>
  signOut: () => void
}

const STORAGE_KEY = 'estudiario-session'

const SessionContext = createContext<SessionContextValue | null>(null)

function loadSession(): StoredSession | null {
  const stored = readJson<StoredSession | null>(STORAGE_KEY, null)
  if (!stored?.token || new Date(stored.expiresAt).getTime() <= Date.now()) {
    removeKey(STORAGE_KEY)
    return null
  }
  return stored
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<StoredSession | null>(() => {
    const stored = loadSession()
    setApiToken(stored?.token ?? null)
    return stored
  })

  const signOut = useCallback(() => {
    removeKey(STORAGE_KEY)
    setApiToken(null)
    setSession(null)
  }, [])

  const signIn = useCallback(async (email: string, password: string) => {
    const result = await usersApi.login({ email: email.trim(), password })
    const next: StoredSession = { token: result.token, expiresAt: result.expiresAt, user: result.user }
    writeJson(STORAGE_KEY, next)
    setApiToken(next.token)
    setSession(next)
  }, [])

  const register = useCallback(
    async (name: string, email: string, password: string) => {
      await usersApi.register({ name: name.trim(), email: email.trim(), password })
      await signIn(email, password)
    },
    [signIn],
  )

  // Un 401 de cualquier pedido autenticado vuelve a la pantalla de ingreso.
  useEffect(() => {
    onUnauthorized(signOut)
    return () => onUnauthorized(null)
  }, [signOut])

  // Al vencer el token se cierra la sesión, aunque no haya pedidos en curso.
  useEffect(() => {
    if (!session) return
    const remaining = new Date(session.expiresAt).getTime() - Date.now()
    const timeout = setTimeout(signOut, Math.max(0, Math.min(remaining, 2 ** 31 - 1)))
    return () => clearTimeout(timeout)
  }, [session, signOut])

  const user = session?.user ?? null
  const value = useMemo(() => ({ user, signIn, register, signOut }), [user, signIn, register, signOut])
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext)
  if (!ctx) throw new Error('useSession debe usarse dentro de SessionProvider')
  return ctx
}
