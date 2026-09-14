import { cn } from '@/lib/utils'
import { HTMLAttributes } from 'react'

export interface BadgeProps extends HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'secondary' | 'outline' | 'success' | 'warning' | 'destructive' | 'danger'
}

const variants: Record<NonNullable<BadgeProps['variant']>, string> = {
  default: 'border-transparent bg-primary/10 text-primary',
  secondary: 'border-transparent bg-secondary text-secondary-foreground',
  outline: 'text-foreground',
  success:
    'border-transparent bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400',
  warning:
    'border-transparent bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-400',
  destructive:
    'border-transparent bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-400',
  danger:
    'border-transparent bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-400',
}

export function Badge({ className, variant = 'default', ...props }: BadgeProps) {
  return (
    <div
      className={cn(
        'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium transition-colors',
        variants[variant],
        className,
      )}
      {...props}
    />
  )
}
