import { useTranslation } from 'react-i18next'
import { Server } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'
import { ServerPresetCard } from '@/components/ServerPresetCard'
import { startHerePresets, type ServerPreset } from '@/data/catalog'
import { cn } from '@/lib/utils'

interface GuidedServerEmptyProps {
  onSelect: (preset: ServerPreset) => void
  onAddNow: (preset: ServerPreset) => void
  onBrowseAll?: () => void
  addingId?: string | null
  className?: string
  /** When true, omit the outer icon chrome (e.g. inside a wizard step). */
  embedded?: boolean
  hideBrowse?: boolean
}

export function GuidedServerEmpty({
  onSelect,
  onAddNow,
  onBrowseAll,
  addingId,
  className,
  embedded,
  hideBrowse,
}: GuidedServerEmptyProps) {
  const { t } = useTranslation()
  const cards = startHerePresets()

  return (
    <div
      className={cn(
        'flex flex-col items-center px-4 py-10 text-center sm:px-8',
        className,
      )}
      data-testid="guided-server-empty"
    >
      {!embedded && (
        <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-secondary">
          <Icon icon={Server} className="h-5 w-5 text-muted-foreground" aria-hidden />
        </div>
      )}
      <p className="text-sm font-semibold text-foreground">
        {embedded ? t('guided.titleEmbedded') : t('guided.title')}
      </p>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">{t('guided.body')}</p>
      <div className="mt-6 grid w-full max-w-3xl gap-3 text-left sm:grid-cols-2 lg:grid-cols-3">
        {cards.map((p) => (
          <ServerPresetCard
            key={p.id}
            preset={p}
            compact
            onSelect={onSelect}
            onAddNow={onAddNow}
            adding={addingId === p.id}
          />
        ))}
      </div>
      {!hideBrowse && onBrowseAll && (
        <Button
          className="mt-6"
          variant="outline"
          onClick={onBrowseAll}
          aria-label={t('guided.browseAria')}
        >
          {t('guided.browseAll')}
        </Button>
      )}
    </div>
  )
}
