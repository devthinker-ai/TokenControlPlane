import { useTranslation } from 'react-i18next'
import { Lock } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'
import { canAutoAdd, type ServerPreset } from '@/data/catalog'

interface ServerPresetCardProps {
  preset: ServerPreset
  onSelect: (preset: ServerPreset) => void
  onAddNow?: (preset: ServerPreset) => void
  adding?: boolean
  compact?: boolean
  className?: string
}

export function ServerPresetCard({
  preset,
  onSelect,
  onAddNow,
  adding,
  compact,
  className,
}: ServerPresetCardProps) {
  const { t } = useTranslation()
  const auto = canAutoAdd(preset)
  const needsKey = preset.auth.type === 'static'

  return (
    <div
      className={cn(
        'group flex flex-col rounded-lg border border-border bg-card text-left transition-all duration-150',
        'hover:border-primary/40 hover:bg-secondary/30',
        'focus-within:ring-2 focus-within:ring-ring',
        compact ? 'p-3' : 'p-3.5',
        className,
      )}
      data-testid={`preset-card-${preset.id}`}
    >
      <button
        type="button"
        className="flex flex-1 flex-col items-start text-left focus-visible:outline-none"
        onClick={() => onSelect(preset)}
        aria-label={t('preset.usePreset', { name: preset.name })}
      >
        <div className="flex w-full items-start justify-between gap-2">
          <span className="text-sm font-semibold tracking-tight">{preset.name}</span>
          <div className="flex shrink-0 items-center gap-1">
            <Badge variant="secondary" className="font-mono text-[10px] uppercase">
              {preset.transport === 'http' ? 'HTTP' : 'stdio'}
            </Badge>
            {preset.verified ? (
              <span
                className="h-2 w-2 rounded-full bg-emerald-500"
                title={t('preset.verified')}
                aria-label={t('preset.verified')}
              />
            ) : (
              <Badge variant="outline" className="text-[10px] text-muted-foreground">
                {t('preset.unverified')}
              </Badge>
            )}
          </div>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">{preset.tagline}</p>
        {needsKey && preset.auth.key_label && (
          <p className="mt-2 flex items-center gap-1 text-[11px] text-muted-foreground">
            <Icon icon={Lock} className="h-3 w-3" />
            {preset.auth.key_label}
          </p>
        )}
        {preset.needs_local && (
          <p className="mt-1.5 text-[11px] text-amber-700 dark:text-amber-400">
            {t('preset.needsLocal')}
          </p>
        )}
      </button>
      {auto && onAddNow && (
        <Button
          type="button"
          size="sm"
          className="mt-3 w-full"
          disabled={adding}
          onClick={(e) => {
            e.stopPropagation()
            onAddNow(preset)
          }}
          aria-label={t('preset.addNowAria', { name: preset.name })}
        >
          {adding ? t('preset.adding') : t('preset.addNow')}
        </Button>
      )}
    </div>
  )
}
