import { request } from './client'
import type { ClubStatus, LoginResult, User } from './types'

// Endpoints de users-service publicados en el api-gateway bajo /api/users.
// register y login son públicos; los de /me exigen el JWT.

const base = '/users'

export const usersApi = {
  /** RF-01: crea la cuenta. No devuelve token: después hay que iniciar sesión. */
  register: (input: { name: string; email: string; password: string }) =>
    request<User>(`${base}/register`, { method: 'POST', body: input, anonymous: true }),

  /** RF-02: devuelve el JWT que se manda en cada pedido. */
  login: (input: { email: string; password: string }) =>
    request<LoginResult>(`${base}/login`, { method: 'POST', body: input, anonymous: true }),

  me: () => request<User>(`${base}/me`),

  /** RF-30: cambia la contraseña (por ejemplo, la generada para los autores de la agencia). */
  changePassword: (input: { currentPassword: string; newPassword: string }) =>
    request<void>(`${base}/me/password`, { method: 'POST', body: input }),

  /** RF-29: estado de la vinculación con el Club de Beneficios, saldo y movimientos. */
  club: () => request<ClubStatus>(`${base}/me/club`),

  /** RF-29: pide vincular la cuenta del Club. El Club le manda un mail a la persona para confirmar. */
  linkClub: () => request<ClubStatus>(`${base}/me/club`, { method: 'POST' }),
}
