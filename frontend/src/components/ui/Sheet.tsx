import { type ReactNode, useEffect } from 'react'
import { X } from 'lucide-react'
import { createPortal } from 'react-dom'

/**
 * Hoja inferior en mobile / diálogo centrado en desktop. Se usa para
 * formularios cortos (crear evento, enviar al chat, editar materia, etc.)
 * sin salir de la pantalla actual.
 */
export function Sheet({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [open, onClose])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center">
      <button
        aria-label="Cerrar"
        className="absolute inset-0 bg-ink/40 backdrop-blur-[1px]"
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="safe-bottom relative flex max-h-[90svh] w-full flex-col overflow-hidden rounded-t-xl border border-border bg-surface shadow-2xl sm:max-w-lg sm:rounded-xl"
      >
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <h2 className="text-lg font-medium">{title}</h2>
          <button
            onClick={onClose}
            aria-label="Cerrar"
            className="flex h-8 w-8 items-center justify-center rounded-full text-muted hover:bg-accent-soft"
          >
            <X size={18} />
          </button>
        </div>
        <div className="overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>,
    document.body,
  )
}
