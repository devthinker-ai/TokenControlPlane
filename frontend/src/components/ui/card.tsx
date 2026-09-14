import { cn } from '@/lib/utils'
import { HTMLAttributes, ReactNode } from 'react'

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'rounded-lg border border-border bg-card text-card-foreground shadow-none',
        className,
      )}
      {...props}
    />
  )
}

export function CardHeader({
  className,
  children,
  action,
  ...props
}: HTMLAttributes<HTMLDivElement> & { action?: ReactNode }) {
  return (
    <div
      className={cn('flex items-start justify-between gap-3 p-5', className)}
      {...props}
    >
      <div className="flex min-w-0 flex-1 flex-col space-y-1">{children}</div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  )
}

export function CardTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h3
      className={cn('text-sm font-semibold leading-none tracking-tight', className)}
      {...props}
    />
  )
}

export function CardDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn('text-sm text-muted-foreground', className)} {...props} />
}

export function CardContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('p-5 pt-0', className)} {...props} />
}

/** Top-right header slot alias (Phase 8). Prefer CardHeader `action` prop. */
export function CardAction({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('shrink-0', className)} {...props} />
}
