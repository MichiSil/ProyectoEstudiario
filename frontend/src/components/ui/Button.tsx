import { forwardRef, type ButtonHTMLAttributes } from 'react'
import clsx from 'clsx'

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md' | 'lg' | 'icon'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  size?: Size
}

const variantClasses: Record<Variant, string> = {
  primary: 'bg-accent text-white hover:opacity-90 active:opacity-80',
  secondary: 'bg-surface-raised text-ink border border-border hover:bg-accent-soft',
  ghost: 'text-muted hover:bg-accent-soft hover:text-ink',
  danger: 'bg-danger text-white hover:opacity-90',
}

const sizeClasses: Record<Size, string> = {
  sm: 'h-8 px-3 text-sm gap-1.5',
  md: 'h-10 px-4 text-sm gap-2',
  lg: 'h-12 px-5 text-base gap-2',
  icon: 'h-10 w-10 shrink-0',
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant = 'primary', size = 'md', ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      className={clsx(
        'inline-flex items-center justify-center rounded-xl font-medium transition-colors',
        'disabled:opacity-50 disabled:pointer-events-none',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        variantClasses[variant],
        sizeClasses[size],
        className,
      )}
      {...props}
    />
  )
})
