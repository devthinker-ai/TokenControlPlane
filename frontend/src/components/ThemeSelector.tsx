import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Monitor, Moon, Sun } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  applyTheme,
  getStoredTheme,
  setTheme,
  systemPrefersDark,
  type Theme,
} from '@/lib/theme'
import { Icon } from '@/components/ui/icon'

function useThemePreference() {
  const [theme, setThemeState] = useState<Theme>(() => getStoredTheme())

  useEffect(() => {
    applyTheme(theme)

    const onStorage = (e: StorageEvent) => {
      if (e.key === 'tcp_theme') setThemeState(getStoredTheme())
    }
    const onCustom = (e: Event) => {
      const detail = (e as CustomEvent<Theme>).detail
      if (detail) setThemeState(detail)
    }
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onSystem = () => {
      if (getStoredTheme() === 'system') applyTheme('system')
    }

    window.addEventListener('storage', onStorage)
    window.addEventListener('mcp-gw-theme', onCustom)
    mq.addEventListener('change', onSystem)
    return () => {
      window.removeEventListener('storage', onStorage)
      window.removeEventListener('mcp-gw-theme', onCustom)
      mq.removeEventListener('change', onSystem)
    }
  }, [theme])

  const choose = (next: Theme) => {
    setTheme(next)
    setThemeState(next)
  }

  return { theme, choose }
}

/** Compact segmented control — Light / Dark / System. */
export function ThemeSelector({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { theme, choose } = useThemePreference()

  const options: { value: Theme; label: string; icon: typeof Sun }[] = [
    { value: 'light', label: t('common.themeLight'), icon: Sun },
    { value: 'dark', label: t('common.themeDark'), icon: Moon },
    { value: 'system', label: t('common.themeSystem'), icon: Monitor },
  ]

  return (
    <div
      role="group"
      aria-label={t('common.themeAria')}
      className={cn(
        'grid grid-cols-3 gap-0.5 rounded-md border border-border bg-secondary/60 p-0.5',
        className,
      )}
    >
      {options.map(({ value, label, icon }) => {
        const active = theme === value
        return (
          <button
            key={value}
            type="button"
            aria-pressed={active}
            title={
              value === 'system'
                ? t('common.systemTheme', {
                    mode: systemPrefersDark() ? t('common.themeDark') : t('common.themeLight'),
                  })
                : label
            }
            onClick={() => choose(value)}
            className={cn(
              'flex h-8 min-w-0 flex-col items-center justify-center gap-0.5 rounded-[5px] px-1 text-[10px] font-medium leading-none transition-colors',
              active
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            <Icon icon={icon} className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{label}</span>
          </button>
        )
      })}
    </div>
  )
}

/** Icon-only cycle for collapsed sidebar: light → dark → system. */
export function ThemeCycleButton({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { theme, choose } = useThemePreference()
  const labelFor = (v: Theme) =>
    v === 'light'
      ? t('common.themeLight')
      : v === 'dark'
        ? t('common.themeDark')
        : t('common.themeSystem')
  const next: Theme =
    theme === 'light' ? 'dark' : theme === 'dark' ? 'system' : 'light'
  const icon = theme === 'light' ? Sun : theme === 'dark' ? Moon : Monitor

  return (
    <button
      type="button"
      className={cn(
        'inline-flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        className,
      )}
      aria-label={t('common.themeCycle', {
        current: labelFor(theme),
        next: labelFor(next),
      })}
      title={`${t('common.theme')}: ${labelFor(theme)}`}
      onClick={() => choose(next)}
    >
      <Icon icon={icon} className="h-4 w-4" />
    </button>
  )
}
