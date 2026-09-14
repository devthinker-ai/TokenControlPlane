import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { getLocale, setLocale, type AppLocale } from '@/i18n'

const OPTIONS: { value: AppLocale; label: string }[] = [
  { value: 'de', label: 'DE' },
  { value: 'en', label: 'EN' },
]

/** Compact DE|EN segmented control (not a dropdown). */
export function LocaleSwitcher({ className }: { className?: string }) {
  const { i18n } = useTranslation()
  const active = getLocale()

  return (
    <div
      role="group"
      aria-label={i18n.language?.startsWith('de') ? 'Sprache' : 'Language'}
      className={cn(
        'inline-grid grid-cols-2 gap-0.5 rounded-md border border-border bg-secondary/60 p-0.5',
        className,
      )}
    >
      {OPTIONS.map(({ value, label }) => {
        const pressed = active === value
        return (
          <button
            key={value}
            type="button"
            aria-pressed={pressed}
            onClick={() => setLocale(value)}
            className={cn(
              'h-8 min-w-[2.25rem] rounded-[5px] px-2 text-xs font-medium tabular-nums transition-colors',
              pressed
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {label}
          </button>
        )
      })}
    </div>
  )
}
