import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

export function ErrorBanner({
  message,
  onRetry,
  className,
}: {
  message: string
  onRetry?: () => void
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <div
      className={cn(
        'flex items-center gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive',
        className,
      )}
      role="alert"
    >
      <Icon icon={AlertTriangle} className="h-3.5 w-3.5 shrink-0" />
      <p className="min-w-0 flex-1">{message}</p>
      {onRetry && (
        <Button variant="ghost" size="sm" onClick={onRetry} className="shrink-0 text-destructive">
          {t('common.retry')}
        </Button>
      )}
    </div>
  )
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        )}
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}
