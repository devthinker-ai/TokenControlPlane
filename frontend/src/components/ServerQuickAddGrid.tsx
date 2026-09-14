import { useTranslation } from 'react-i18next'
import { ServerPresetCard } from '@/components/ServerPresetCard'
import { presetsByCategory, type ServerCategory, type ServerPreset } from '@/data/catalog'

interface ServerQuickAddGridProps {
  onSelect: (preset: ServerPreset) => void
  onAddNow: (preset: ServerPreset) => void
  addingId?: string | null
}

const CATEGORY_KEY: Record<ServerCategory, string> = {
  search: 'preset.categorySearch',
  docs: 'preset.categoryDocs',
  data: 'preset.categoryDocs',
  dev: 'preset.categoryDev',
  productivity: 'preset.categoryProductivity',
  browser: 'preset.categoryBrowser',
  local: 'preset.categoryLocal',
}

export function ServerQuickAddGrid({ onSelect, onAddNow, addingId }: ServerQuickAddGridProps) {
  const { t } = useTranslation()
  const groups = presetsByCategory()

  return (
    <div className="max-h-[60vh] space-y-5 overflow-y-auto pr-1" data-testid="quick-add-grid">
      {groups.map((g) => (
        <section key={g.category} className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {t(CATEGORY_KEY[g.category])}
          </h3>
          <div className="grid gap-2 sm:grid-cols-2">
            {g.presets.map((p) => (
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
        </section>
      ))}
    </div>
  )
}
