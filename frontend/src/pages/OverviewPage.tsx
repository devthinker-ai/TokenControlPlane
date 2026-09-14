import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { api, type ToolUsage, type UsageSummary } from '@/lib/api'
import { cn } from '@/lib/utils'
import { fmtNumber, fmtTokens } from '@/lib/fmt'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { SkeletonRows } from '@/components/ui/skeleton'
import { GuidedServerEmpty } from '@/components/GuidedServerEmpty'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { Button } from '@/components/ui/button'
import { canAutoAdd, type ServerPreset } from '@/data/catalog'
import { createInputFromPreset } from '@/lib/createInputFromPreset'
import { useToast } from '@/components/Toast'

const CHART_PRIMARY = 'hsl(221 100% 45%)' // chart constant (token primary)

export function OverviewPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { push } = useToast()
  const [usage, setUsage] = useState<UsageSummary | null>(null)
  const [tools, setTools] = useState<ToolUsage[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [addingId, setAddingId] = useState<string | null>(null)

  const load = async () => {
    setError('')
    setLoading(true)
    try {
      const [u, t] = await Promise.all([api.usage(), api.usageTools()])
      setUsage(u)
      setTools(t.slice(0, 5))
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('overview.loadFailed'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const addPreset = async (preset: ServerPreset) => {
    if (!canAutoAdd(preset)) {
      navigate('/servers')
      return
    }
    setAddingId(preset.id)
    try {
      const res = await api.createServer(createInputFromPreset(preset))
      if (res.indexing_error) {
        push({ title: res.indexing_error, tone: 'warning' })
      } else {
        push({ title: t('overview.added', { name: preset.name }), tone: 'success' })
      }
      await load()
      navigate('/servers')
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : t('overview.addFailed'),
        tone: 'warning',
      })
      navigate('/servers')
    } finally {
      setAddingId(null)
    }
  }

  const chartData =
    usage?.daily.map((d) => ({
      date: d.date.slice(5),
      tokens: d.tokens,
    })) ?? []

  const budget = usage?.monthly_budget ?? 0
  const used = usage?.tokens_used ?? 0
  const budgetPct = budget > 0 ? (used / budget) * 100 : 0
  const llm = usage?.llm
  const llmExactPct =
    llm && llm.tokens > 0 ? Math.round((llm.tokens_exact / llm.tokens) * 100) : 0
  const llmEstPct =
    llm && llm.tokens > 0 ? Math.round((llm.tokens_estimated / llm.tokens) * 100) : 0

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('overview.title')}
        description={t('overview.description')}
        actions={
          <Button size="sm" onClick={() => navigate('/servers')}>
            {t('overview.addServer')}
          </Button>
        }
      />

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}
      {loading && <SkeletonRows rows={4} />}

      {!loading && usage && (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <MetricCard label={t('overview.servers')} value={String(usage.servers_count ?? 0)} />
            <MetricCard label={t('overview.activeKeys')} value={String(usage.active_keys ?? 0)} />
            <MetricCard
              label={t('overview.requestsToday')}
              value={fmtNumber(usage.requests_today)}
            />
            <MetricCard
              label={t('overview.budgetUsed')}
              value={`${Math.min(100, budgetPct).toFixed(0)}%`}
              footer={
                <div className="mt-3 h-1 overflow-hidden rounded-full bg-secondary">
                  <div
                    className={cn(
                      'h-full rounded-full transition-all',
                      budgetPct > 100 ? 'bg-destructive' : 'bg-primary',
                    )}
                    style={{ width: `${Math.min(100, budgetPct)}%` }}
                  />
                </div>
              }
            />
          </div>

          <p className="text-sm text-muted-foreground">
            {t('overview.mcpTraffic', { tokens: fmtTokens(used) })}
            {llm && llm.tokens > 0 && (
              <>
                {' · '}
                {t('overview.llmTraffic', {
                  count: llm.providers,
                  tokens: fmtTokens(llm.tokens),
                  exact: llmExactPct,
                  estimated: llmEstPct,
                  providers: llm.providers,
                })}
              </>
            )}
          </p>

          {(usage.servers_count ?? 0) === 0 && (
            <div className="rounded-lg border border-border">
              <GuidedServerEmpty
                onSelect={(p) => {
                  if (canAutoAdd(p)) void addPreset(p)
                  else navigate('/servers')
                }}
                onAddNow={(p) => void addPreset(p)}
                onBrowseAll={() => navigate('/servers')}
                addingId={addingId}
              />
            </div>
          )}

          {(usage.servers_count ?? 0) > 0 && (
            <>
              <Card>
                <CardHeader>
                  <CardTitle>{t('overview.dailyTokens')}</CardTitle>
                  <CardDescription>
                    {t('overview.dailyTokensDesc', { budget: fmtTokens(budget) })}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="h-[260px] w-full">
                    <ResponsiveContainer width="100%" height="100%">
                      <AreaChart data={chartData}>
                        <defs>
                          <linearGradient id="tokenFill" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor={CHART_PRIMARY} stopOpacity={0.08} />
                            <stop offset="100%" stopColor={CHART_PRIMARY} stopOpacity={0} />
                          </linearGradient>
                        </defs>
                        <CartesianGrid
                          strokeDasharray="3 3"
                          stroke="hsl(var(--border))"
                          vertical={false}
                        />
                        <XAxis
                          dataKey="date"
                          tick={{ fill: 'hsl(var(--muted-foreground))', fontSize: 12, dy: 8 }}
                          axisLine={false}
                          tickLine={false}
                        />
                        <YAxis
                          tick={{ fill: 'hsl(var(--muted-foreground))', fontSize: 12 }}
                          axisLine={false}
                          tickLine={false}
                          tickFormatter={fmtTokens}
                        />
                        <Tooltip
                          contentStyle={{
                            background: 'hsl(var(--card))',
                            border: '1px solid hsl(var(--border))',
                            borderRadius: 6,
                            fontSize: 12,
                            boxShadow: '0 4px 12px rgba(0,0,0,0.08)',
                          }}
                        />
                        <Area
                          type="monotone"
                          dataKey="tokens"
                          name={t('overview.tokens')}
                          stroke={CHART_PRIMARY}
                          fill="url(#tokenFill)"
                          strokeWidth={2}
                          isAnimationActive
                          animationDuration={300}
                        />
                      </AreaChart>
                    </ResponsiveContainer>
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>{t('overview.topTools')}</CardTitle>
                  <CardDescription>{t('overview.topToolsDesc')}</CardDescription>
                </CardHeader>
                <CardContent>
                  {tools.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{t('overview.noToolCalls')}</p>
                  ) : (
                    <dl className="divide-y divide-border">
                      {tools.map((tool) => (
                        <div
                          key={tool.name}
                          className="flex items-center justify-between gap-4 py-2.5 text-sm"
                        >
                          <dt className="min-w-0 truncate font-medium">
                            {tool.name}
                            {tool.server_name && (
                              <span className="ml-2 font-normal text-muted-foreground">
                                {tool.server_name}
                              </span>
                            )}
                          </dt>
                          <dd className="tabular-nums text-muted-foreground">
                            {fmtNumber(tool.call_count)}
                          </dd>
                        </div>
                      ))}
                    </dl>
                  )}
                </CardContent>
              </Card>
            </>
          )}
        </>
      )}
    </div>
  )
}

function MetricCard({
  label,
  value,
  footer,
}: {
  label: string
  value: string
  footer?: React.ReactNode
}) {
  return (
    <Card>
      <CardContent className="p-5">
        <p className="text-sm text-muted-foreground">{label}</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums tracking-tight">{value}</p>
        {footer}
      </CardContent>
    </Card>
  )
}
