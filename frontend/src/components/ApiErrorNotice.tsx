import { AlertCircle } from 'lucide-react'
import { ApiError, errorMessage } from '@/api/client'

/**
 * Muestra un error de la API: el mensaje, cada regla incumplida con su
 * identificador del SPEC (RN-xx) y el correlation id para buscarlo en los logs.
 */
export function ApiErrorNotice({ error, sessionLabel }: { error: unknown; sessionLabel?: (index: number) => string }) {
  if (!error) return null
  const apiError = error instanceof ApiError ? error : null

  return (
    <div role="alert" className="animate-fade-in rounded-xl bg-danger/10 px-4 py-3 text-sm text-ink">
      <p className="flex items-start gap-2 font-medium">
        <AlertCircle size={16} className="mt-0.5 shrink-0 text-danger" />
        {errorMessage(error)}
      </p>
      {!!apiError?.violations.length && (
        <ul className="mt-2 flex flex-col gap-1 pl-6">
          {apiError.violations.map((v, i) => (
            <li key={i} className="text-[13px] text-muted">
              <span className="mr-1.5 rounded-md bg-danger/15 px-1.5 py-0.5 text-[11px] font-bold text-danger">{v.rule}</span>
              {v.sessionIndex >= 0 && sessionLabel && <span className="font-medium text-ink">{sessionLabel(v.sessionIndex)}: </span>}
              {v.message}
            </li>
          ))}
        </ul>
      )}
      {apiError?.correlationId && (
        <p className="mt-2 pl-6 font-mono text-[11px] text-subtle">correlationId {apiError.correlationId}</p>
      )}
    </div>
  )
}
