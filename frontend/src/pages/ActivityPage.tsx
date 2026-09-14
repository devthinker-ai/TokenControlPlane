import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Activity,
  AlertTriangle,
  CreditCard,
  KeyRound,
  Server,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { api, type ActivityEvent } from '@/lib/api'
import { dayLabel, formatRelative, cn } from '@/lib/utils'
import { EmptyState } from '@/components/EmptyState'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { SkeletonRows } from '@/components/ui/skeleton'
import { Button } from '@/components/ui/button'
import { Icon } from '@/components/ui/icon'

const FILTER_KEYS = [
  { value: '', labelKey: 'activity.filterAll' },
  { value: 'key_killed_auto', labelKey: 'activity.filterAutoKill' },
  { value: 'key_killed_manual', labelKey: 'activity.filterManualKill' },
  { value: 'key_created', labelKey: 'activity.filterKeys' },
  { value: 'server_added', labelKey: 'activity.filterServers' },
  { value: 'plan_changed', labelKey: 'activity.filterPlan' },
  { value: 'budget_exceeded', labelKey: 'activity.filterBudget' },
  { value: 'rate_limited', labelKey: 'activity.filterRateLimit' },
]

function iconFor(kind: string): LucideIcon {
  if (kind.startsWith('key_')) return KeyRound
  if (kind.startsWith('server_')) return Server
  if (kind.includes('plan')) return CreditCard
  if (kind.includes('circuit') || kind.includes('rate')) return Zap
  if (kind.includes('budget') || kind.includes('error')) return AlertTriangle
  return Activity
}

export function ActivityPage() {
  const { t } = useTranslation()
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [kind, setKind] = useState('')
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 5 * 60_000)
    return () => window.clearInterval(id)
  }, [])

  const load = useCallback(async () => {
    setError('')
    try {
      setEvents(await api.activity(50))
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('activity.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const filtered = useMemo(
    () => (kind ? events.filter((e) => e.kind === kind) : events),
    [events, kind],
  )

  const groups = useMemo(() => {
    const map = new Map<string, ActivityEvent[]>()
    for (const e of filtered) {
      const label = dayLabel(e.created_at)
      const list = map.get(label) ?? []
      list.push(e)
      map.set(label, list)
    }
    return [...map.entries()]
  }, [filtered])

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('activity.title')}
        description={t('activity.description')}
        actions={
          <Button variant="outline" size="sm" onClick={() => void load()} aria-label={t('activity.refreshAria')}>
            {t('activity.refresh')}
          </Button>
        }
      />

      <div className="inline-flex flex-wrap rounded-md border border-border p-0.5">
        {FILTER_KEYS.map((f) => (
          <button
            key={f.value || 'all'}
            type="button"
            onClick={() => setKind(f.value)}
            className={cn(
              'h-7 rounded px-2.5 text-xs font-medium transition-colors',
              kind === f.value
                ? 'bg-secondary text-foreground'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {t(f.labelKey)}
          </button>
        ))}
      </div>

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}
      {loading && <SkeletonRows rows={5} />}

      {!loading && filtered.length === 0 && (
        <EmptyState
          icon={Activity}
          title={t('activity.emptyTitle')}
          description={t('activity.emptyDesc')}
        />
      )}

      {!loading &&
        groups.map(([label, items]) => (
          <section key={label} className="space-y-2">
            <h2 className="sticky top-0 z-[1] bg-background py-1 text-xs font-medium text-muted-foreground">
              {label}
            </h2>
            <ul className="divide-y divide-border rounded-lg border border-border">
              {items.map((e) => {
                const Ico = iconFor(e.kind)
                return (
                  <li
                    key={e.id}
                    className="flex items-start gap-3 px-4 py-3"
                  >
                    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-muted">
                      <Icon icon={Ico} className="h-3.5 w-3.5 text-muted-foreground" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium">{e.summary}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        {e.kind}
                        {e.subject ? ` · ${e.subject}` : ''}
                      </p>
                    </div>
                    <time
                      className="shrink-0 text-xs tabular-nums text-muted-foreground"
                      title={e.created_at}
                      dateTime={e.created_at}
                    >
                      {formatRelative(e.created_at, now)}
                    </time>
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
    </div>
  )
}
