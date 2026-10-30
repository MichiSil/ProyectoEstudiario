import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react'
import clsx from 'clsx'

const fieldClasses =
  'w-full min-w-0 rounded-xl border border-border bg-paper px-3 py-2.5 text-sm text-ink placeholder:text-subtle focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-soft'

export function Label({ children }: { children: ReactNode }) {
  return <label className="mb-1.5 block text-sm font-medium text-ink">{children}</label>
}

const dateTimeTypes = new Set(['date', 'time', 'datetime-local', 'month', 'week'])

export function Input({ className, type, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      type={type}
      className={clsx(fieldClasses, type && dateTimeTypes.has(type) && 'block h-[2.625rem] appearance-none text-left', className)}
      {...props}
    />
  )
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
  return <div className="mb-4 min-w-0">{children}</div>
}
