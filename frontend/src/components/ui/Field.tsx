import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react'
import clsx from 'clsx'

const fieldClasses =
  'w-full rounded-xl border border-border bg-paper px-3 py-2.5 text-sm text-ink placeholder:text-subtle focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-soft'

export function Label({ children }: { children: ReactNode }) {
  return <label className="mb-1.5 block text-sm font-medium text-ink">{children}</label>
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={clsx(fieldClasses, className)} {...props} />
}

export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={clsx(fieldClasses, 'min-h-24 resize-y', className)} {...props} />
}

export function Select({ className, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select className={clsx(fieldClasses, className)} {...props}>
      {children}
    </select>
  )
}

export function FieldGroup({ children }: { children: ReactNode }) {
  return <div className="mb-4">{children}</div>
}
