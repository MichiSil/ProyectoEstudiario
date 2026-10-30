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
  EXAM_CANCELLED: 'Este examen está cancelado: no se puede editar ni planificar.',
  FORBIDDEN: 'Esta sección es sólo para administradores.',
  ALREADY_MODERATED: 'Otro administrador ya moderó esta publicación.',
  NOT_APPROVED: 'Sólo se pueden calificar resúmenes publicados.',
  OWN_SUMMARY: 'No podés calificar tu propio resumen.',
  FILE_TOO_LARGE: 'El archivo pesa más de 10 MB.',
  UNSUPPORTED_FILE_TYPE: 'El archivo tiene que ser un PDF.',
  SEARCH_UNAVAILABLE: 'La búsqueda no está disponible en este momento.',
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
  VALIDATION_ERROR: 'Hay datos inválidos en el formulario.',
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
  const res = await send(path, { method, body, headers, anonymous })
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
        : // Nunca se muestra el detalle técnico del servidor: si el código no tiene
          // mensaje propio, se usa uno general en castellano.
          (CODE_MESSAGES[code] ??
          (res.status >= 500 ? 'El servidor no está respondiendo.' : 'No pudimos completar la operación.'))
    throw new ApiError(res.status, code, message, violations, problem.correlationId ?? correlationId)
  }
  return data as T
}

async function send(path: string, { method = 'GET', body, headers, anonymous = false }: RequestOptions): Promise<Response> {
  const isForm = body instanceof FormData
  try {
    return await fetch(`/api${path}`, {
      method,
      headers: {
        Accept: 'application/json',
        ...(body !== undefined && !isForm ? { 'Content-Type': 'application/json' } : {}),
        ...(anonymous ? {} : authHeaders()),
        ...headers,
      },
      body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'NETWORK', 'No hay conexión con el servidor. Revisá que el sistema esté levantado.', [], null)
  }
}

/**
 * Descarga un archivo protegido (por ejemplo el PDF de un resumen). Un link
 * común no sirve porque no manda el token: se baja con fetch y se ofrece al
 * navegador como archivo.
 */
export async function downloadFile(path: string, fallbackName: string): Promise<void> {
  const res = await send(path, { headers: { Accept: '*/*' } })
  if (!res.ok) {
    // Reusa el manejo de errores de request (mensajes, 401, etc.).
    await request(path)
    return
  }
  const disposition = res.headers.get('Content-Disposition') ?? ''
  const name = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(disposition)?.[1]
  const url = URL.createObjectURL(await res.blob())
  const a = document.createElement('a')
  a.href = url
  a.download = name ? decodeURIComponent(name) : fallbackName
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
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

/** Qué puede hacer el estudiante ante cada código, para que ningún error quede sin explicación. */
const CODE_HINTS: Record<string, string> = {
  ALREADY_MODERATED: 'La lista se actualizó con el estado nuevo.',
  FILE_TOO_LARGE: 'Probá comprimir el PDF o dividirlo en partes.',
  SEARCH_UNAVAILABLE: 'Probá de nuevo en unos segundos.',
  FORBIDDEN: 'Si sos administradora o administrador, cerrá sesión y volvé a entrar.',
  NETWORK: 'Puede que el backend no esté levantado o que no tengas conexión. Probá de nuevo en unos segundos.',
  NOT_FOUND: 'Puede que ese dato se haya borrado o que lo hayas cargado con otra cuenta. Recargá la página.',
  SERVICE_UNAVAILABLE: 'La base de datos no respondió a tiempo. No se guardó nada: podés reintentar sin miedo.',
  UPSTREAM_UNAVAILABLE: 'El api-gateway no pudo hablar con el servicio. Revisá que el backend esté levantado.',
  UPSTREAM_TIMEOUT: 'El servicio tardó demasiado. Probá de nuevo en unos segundos.',
  TIMEOUT: 'El servicio tardó demasiado. Probá de nuevo en unos segundos.',
  INTERNAL_ERROR:
    'Algo falló de nuestro lado. Probá de nuevo en unos segundos; si se repite, avisanos.',
  MALFORMED_REQUEST: 'Revisá los datos del formulario y volvé a intentar.',
}

export function errorHint(err: ApiError): string | undefined {
  if (CODE_HINTS[err.code]) return CODE_HINTS[err.code]
  if (err.code.startsWith('HTTP_')) {
    return err.status >= 500
      ? 'No pudimos comunicarnos con el servidor. Revisá que el backend esté levantado y probá de nuevo.'
      : 'Revisá los datos y probá de nuevo.'
  }
  return undefined
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'Ocurrió un error inesperado.'
}
