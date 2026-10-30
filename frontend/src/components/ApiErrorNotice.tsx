import { useEffect } from 'react'
import { AlertCircle } from 'lucide-react'
import { ApiError, errorHint, errorMessage } from '@/api/client'

/**
 * Muestra un error de la API en lenguaje del estudiante: qué pasó, qué puede
 * hacer y, si corresponde, cada regla incumplida explicada. Los datos técnicos
 * (código, regla del SPEC y correlationId para buscar en los logs) no se
 * muestran en pantalla: quedan en la consola del navegador para el equipo.
 */
export function ApiErrorNotice({
  error,
  hint,
  sessionLabel,
}: {
  error: unknown
  /** Qué puede hacer el estudiante; reemplaza a la explicación genérica del código. */
  hint?: string
  sessionLabel?: (index: number) => string
}) {
  useEffect(() => {
    if (error instanceof ApiError) {
      console.error('[api]', error.status, error.code, {
        correlationId: error.correlationId,
        reglas: error.violations.map((v) => v.rule),
      })
    }
  }, [error])

  if (!error) return null
  const apiError = error instanceof ApiError ? error : null
  const explanation = hint ?? (apiError ? errorHint(apiError) : undefined)

  return (
    <div role="alert" className="animate-fade-in rounded-xl bg-danger/10 px-4 py-3 text-sm text-ink">
      <p className="flex items-start gap-2 font-medium">
        <AlertCircle size={16} className="mt-0.5 shrink-0 text-danger" />
        {errorMessage(error)}
      </p>
      {explanation && <p className="mt-1 pl-6 text-[13px] text-muted">{explanation}</p>}
      {!!apiError?.violations.length && (
        <ul className="mt-2 flex flex-col gap-1 pl-6">
          {apiError.violations.map((v, i) => (
            <li key={i} className="text-[13px] text-muted">
              {v.sessionIndex >= 0 && sessionLabel && <span className="font-medium text-ink">{sessionLabel(v.sessionIndex)}: </span>}
              {v.message.charAt(0).toUpperCase() + v.message.slice(1)}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
