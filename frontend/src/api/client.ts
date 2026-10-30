import type { FieldError, RuleViolation } from './types'

/**
 * Cliente HTTP del frontend. Todo pasa por el api-gateway (/api/...), que
 * agrega el X-Correlation-Id si falta y reenvía al servicio (ADR-011).
 *
 * Identidad: el JWT que emite users-service viaja en Authorization: Bearer.
 * El gateway lo valida y completa X-User-Id para el servicio; el frontend
 * nunca manda X-User-Id (el gateway lo descarta). Si el gateway responde 401,
 * el token venció o no es válido y la sesión se cierra (onUnauthorized).
 */

let currentToken: string | null = null
let unauthorizedHandler: (() => void) | null = null

export function setApiToken(token: string | null) {
  currentToken = token
}

/** Lo registra SessionProvider para volver a la pantalla de ingreso ante un 401. */
export function onUnauthorized(handler: (() => void) | null) {
  unauthorizedHandler = handler
}

function authHeaders(): Record<string, string> {
  return currentToken ? { Authorization: `Bearer ${currentToken}` } : {}
}

/** Error de la API en formato Problem Details (RFC 9457, ADR-005). */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly violations: RuleViolation[]
  readonly correlationId: string | null

  constructor(status: number, code: string, message: string, violations: RuleViolation[], correlationId: string | null) {
    super(message)
    this.status = status
    this.code = code
    this.violations = violations
    this.correlationId = correlationId
  }
}

/** Mensajes en castellano para los códigos que devuelven el gateway, users-service y planner-service. */
const CODE_MESSAGES: Record<string, string> = {
  UNAUTHENTICATED: 'Tu sesión venció o no es válida. Volvé a ingresar.',
  INVALID_CREDENTIALS: 'Mail o contraseña incorrectos.',
  EMAIL_IN_USE: 'Ya existe una cuenta con ese mail. Iniciá sesión.',
  NOT_FOUND: 'No encontramos lo que buscabas.',
  BUSINESS_RULE_VIOLATION: 'La operación incumple reglas del plan.',
  PLAN_NOT_PROPOSED: 'Esta propuesta ya no se puede confirmar.',
  PLAN_ALREADY_CONFIRMED: 'Este examen ya tiene un plan confirmado.',
  REQUEST_IN_PROGRESS: 'La confirmación ya se está procesando. Esperá unos segundos.',
  IDEMPOTENCY_KEY_REUSED: 'La confirmación se mezcló con otro plan. Volvé a pedir la propuesta.',
  SERVICE_UNAVAILABLE: 'El servicio no está disponible en este momento. Probá de nuevo en unos segundos.',
  TIMEOUT: 'La operación tardó demasiado. Probá de nuevo.',
  UPSTREAM_UNAVAILABLE: 'El servidor no está disponible.',
  UPSTREAM_TIMEOUT: 'El servidor no respondió a tiempo.',
  MALFORMED_REQUEST: 'Los datos enviados no tienen el formato esperado.',
  INVALID_DATE: 'La fecha no tiene un formato válido.',
  INVALID_TIMEZONE: 'La zona horaria no es válida.',
  INVALID_SLOT: 'Alguna franja horaria no es válida.',
  INTERNAL_ERROR: 'Ocurrió un error inesperado en el servidor.',
}

interface RequestOptions {
  method?: string
  body?: unknown
  headers?: Record<string, string>
  /** Pedido sin token (registro e inicio de sesión): un 401 no cierra la sesión. */
  anonymous?: boolean
}

export async function request<T>(
  path: string,
  { method = 'GET', body, headers, anonymous = false }: RequestOptions = {},
): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api${path}`, {
      method,
      headers: {
        Accept: 'application/json',
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...(anonymous ? {} : authHeaders()),
        ...headers,
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError(0, 'NETWORK', 'No hay conexión con el servidor. Revisá que el sistema esté levantado.', [], null)
  }

  const correlationId = res.headers.get('X-Correlation-Id')
  if (res.status === 204) return undefined as T

  const text = await res.text()
  const data = text ? safeJson(text) : null

  if (!res.ok) {
    const problem = (data ?? {}) as {
      code?: string
      detail?: string
      title?: string
      errors?: (RuleViolation | FieldError)[]
      correlationId?: string
    }
    const code = problem.code ?? `HTTP_${res.status}`
    if (res.status === 401 && !anonymous) unauthorizedHandler?.()

    const errors = problem.errors ?? []
    const fieldErrors = errors.filter((e): e is FieldError => 'field' in e)
    const violations = errors.filter((e): e is RuleViolation => 'rule' in e)
    const message =
      code === 'VALIDATION_ERROR' && fieldErrors.length > 0
        ? fieldErrors.map((e) => capitalize(e.message)).join('. ') + '.'
        : (CODE_MESSAGES[code] ?? problem.detail ?? problem.title ?? `Error ${res.status}`)
    throw new ApiError(res.status, code, message, violations, problem.correlationId ?? correlationId)
  }
  return data as T
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

function safeJson(text: string): unknown {
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'Ocurrió un error inesperado.'
}
