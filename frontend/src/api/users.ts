import { request } from './client'
import type { LoginResult, User } from './types'

// Endpoints de users-service publicados en el api-gateway bajo /api/users.
// register y login son públicos; me exige el JWT.

const base = '/users'

export const usersApi = {
  /** RF-01: crea la cuenta. No devuelve token: después hay que iniciar sesión. */
  register: (input: { name: string; email: string; password: string }) =>
    request<User>(`${base}/register`, { method: 'POST', body: input, anonymous: true }),

  /** RF-02: devuelve el JWT que se manda en cada pedido. */
  login: (input: { email: string; password: string }) =>
    request<LoginResult>(`${base}/login`, { method: 'POST', body: input, anonymous: true }),

  me: () => request<User>(`${base}/me`),
}
