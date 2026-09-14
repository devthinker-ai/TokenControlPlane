import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  api,
  type APIKey,
  type CatalogTool,
  type KeyProviderUsage,
  type KeyServerUsage,
  type LLMProvider,
  type MCPServer,
} from '@/lib/api'
import { formatTokens, cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Icon } from '@/components/ui/icon'
import { useToast } from '@/components/Toast'
import { X } from 'lucide-react'

interface Props {
  keyRow: APIKey
  toolPolicy: boolean
  onClose: () => void
  onSaved: () => void
}

export function KeyDetailDrawer({ keyRow, toolPolicy, onClose, onSaved }: Props) {
  const { push } = useToast()
  const [tab, setTab] = useState<'servers' | 'providers' | 'tools' | 'usage'>('servers')
  const [servers, setServers] = useState<MCPServer[]>([])
  const [serverMode, setServerMode] = useState<'all' | 'custom'>('all')
  const [selectedServers, setSelectedServers] = useState<Set<string>>(new Set())
  const [budgets, setBudgets] = useState<Record<string, string>>({})
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [providerMode, setProviderMode] = useState<'all' | 'custom'>('all')
  const [selectedProviders, setSelectedProviders] = useState<Set<string>>(new Set())
  const [providerBudgets, setProviderBudgets] = useState<Record<string, string>>({})
  const [toolMode, setToolMode] = useState<'all' | 'custom'>('all')
  const [toolServerId, setToolServerId] = useState('')
  const [catalog, setCatalog] = useState<CatalogTool[]>([])
  const [allowedTools, setAllowedTools] = useState<Set<string>>(new Set())
  const [toolSearch, setToolSearch] = useState('')
  const [usage, setUsage] = useState<KeyServerUsage[]>([])
  const [providerUsage, setProviderUsage] = useState<KeyProviderUsage[]>([])
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      const [srvList, srvAccess, toolsAccess, u, provList, provAccess, pu] = await Promise.all([
        api.listServers(),
        api.getKeyServers(keyRow.id),
        api.getKeyTools(keyRow.id),
        api.usageKeyServers(keyRow.id),
        api.listLLMProviders(),
        api.getKeyProviders(keyRow.id),
        api.usageKeyProviders(keyRow.id),
      ])
      setServers(srvList)
      setServerMode(srvAccess.mode === 'custom' ? 'custom' : 'all')
      setSelectedServers(new Set(srvAccess.allowed.map((a) => a.id)))
      const b: Record<string, string> = {}
      for (const a of srvAccess.allowed) {
        if (a.monthly_budget > 0) b[a.id] = String(a.monthly_budget)
      }
      for (const row of u) {
        if (row.monthly_budget > 0 && !b[row.server_id]) {
          b[row.server_id] = String(row.monthly_budget)
        }
      }
      setBudgets(b)
      setProviders(provList)
      setProviderMode(provAccess.mode === 'custom' ? 'custom' : 'all')
      setSelectedProviders(new Set(provAccess.allowed.map((a) => a.id)))
      const pb: Record<string, string> = {}
      for (const a of provAccess.allowed) {
        if (a.monthly_budget > 0) pb[a.id] = String(a.monthly_budget)
      }
      for (const row of pu) {
        if (row.monthly_budget > 0 && !pb[row.provider_id]) {
          pb[row.provider_id] = String(row.monthly_budget)
        }
      }
      setProviderBudgets(pb)
      setToolMode(toolsAccess.mode === 'custom' ? 'custom' : 'all')
      setAllowedTools(new Set(toolsAccess.allowed || []))
      setUsage(u)
      setProviderUsage(pu)
      if (srvList.length && !toolServerId) setToolServerId(srvList[0].id)
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Failed to load key access',
        tone: 'warning',
      })
    }
  }, [keyRow.id, push, toolServerId])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!toolServerId) return
    void api.listServerTools(toolServerId).then(setCatalog).catch(console.error)
  }, [toolServerId])

  const filteredTools = useMemo(() => {
    const q = toolSearch.trim().toLowerCase()
    if (!q) return catalog.filter((t) => t.enabled)
    return catalog.filter(
      (t) =>
        t.enabled &&
        (t.name.toLowerCase().includes(q) ||
          (t.description || '').toLowerCase().includes(q)),
    )
  }, [catalog, toolSearch])

  const saveServers = async () => {
    if (!toolPolicy) {
      push({ title: 'Server access control is a Pro feature', tone: 'warning' })
      return
    }
    setSaving(true)
    try {
      const budgetNums: Record<string, number> = {}
      for (const [sid, raw] of Object.entries(budgets)) {
        const n = Number(raw)
        if (Number.isFinite(n) && n > 0) budgetNums[sid] = Math.floor(n)
      }
      await api.putKeyServers(
        keyRow.id,
        serverMode,
        serverMode === 'custom' ? [...selectedServers] : [],
        budgetNums,
      )
      push({ title: 'Server access saved', tone: 'success' })
      onSaved()
      await load()
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Save failed',
        tone: 'warning',
      })
    } finally {
      setSaving(false)
    }
  }

  const saveProviders = async () => {
    if (!toolPolicy) {
      push({ title: 'Provider access control is a Pro feature', tone: 'warning' })
      return
    }
    setSaving(true)
    try {
      const budgetNums: Record<string, number> = {}
      for (const [pid, raw] of Object.entries(providerBudgets)) {
        const n = Number(raw)
        if (Number.isFinite(n) && n > 0) budgetNums[pid] = Math.floor(n)
      }
      await api.putKeyProviders(
        keyRow.id,
        providerMode,
        providerMode === 'custom' ? [...selectedProviders] : [],
        budgetNums,
      )
      push({ title: 'Provider access saved', tone: 'success' })
      onSaved()
      await load()
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Save failed',
        tone: 'warning',
      })
    } finally {
      setSaving(false)
    }
  }

  const saveTools = async () => {
    if (!toolPolicy) {
      push({ title: 'Tool control is a Pro feature', tone: 'warning' })
      return
    }
    setSaving(true)
    try {
      await api.putKeyTools(
        keyRow.id,
        toolMode,
        toolMode === 'custom' ? [...allowedTools] : [],
      )
      push({ title: 'Tool access saved', tone: 'success' })
      onSaved()
      await load()
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Save failed',
        tone: 'warning',
      })
    } finally {
      setSaving(false)
    }
  }

  const toggleServer = (id: string) => {
    setSelectedServers((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleProvider = (id: string) => {
    setSelectedProviders((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleTool = (name: string) => {
    setAllowedTools((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/30" onClick={onClose}>
      <aside
        className="flex h-full w-full max-w-xl flex-col border-l border-border bg-background shadow-xl"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label={`${keyRow.name} access`}
      >
        <div className="flex items-start justify-between border-b border-border px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold tracking-tight">{keyRow.name}</h2>
            <p className="mt-0.5 text-xs text-muted-foreground">Key access &amp; usage</p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <Icon icon={X} className="h-4 w-4" />
          </Button>
        </div>

        <div className="flex gap-1 border-b border-border px-5 pt-2">
          {(['servers', 'providers', 'tools', 'usage'] as const).map((t) => (
            <button
              key={t}
              type="button"
              className={cn(
                'border-b-2 px-3 py-2 text-sm font-medium capitalize transition-colors',
                tab === t
                  ? 'border-foreground text-foreground'
                  : 'border-transparent text-muted-foreground hover:text-foreground',
              )}
              onClick={() => setTab(t)}
            >
              {t}
            </button>
          ))}
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4">
          {tab === 'servers' && (
            <div className="space-y-4">
              {!toolPolicy && (
                <p className="text-xs text-muted-foreground">
                  Server allowlists are a Pro feature — viewing usage is free.
                </p>
              )}
              <p className="text-xs text-muted-foreground">
                Optional per-server token caps for this key. Empty = unlimited; over the
                cap the next request returns 402.
              </p>
              <div className="flex gap-2">
                {(['all', 'custom'] as const).map((m) => (
                  <button
                    key={m}
                    type="button"
                    disabled={!toolPolicy}
                    className={cn(
                      'rounded-md border px-3 py-1.5 text-xs font-medium',
                      serverMode === m
                        ? 'border-foreground bg-secondary'
                        : 'border-border text-muted-foreground',
                    )}
                    onClick={() => setServerMode(m)}
                  >
                    {m === 'all' ? 'All servers' : 'Custom'}
                  </button>
                ))}
              </div>
              <ul className="space-y-2">
                {servers.map((s) => {
                  const checked = serverMode === 'all' || selectedServers.has(s.id)
                  const used = usage.find((u) => u.server_id === s.id)
                  const budgetN = Number(budgets[s.id] || 0)
                  const tokens = used?.tokens ?? 0
                  const pct = budgetN > 0 ? tokens / budgetN : 0
                  return (
                    <li
                      key={s.id}
                      className={cn(
                        'rounded-md border border-border px-3 py-2',
                        pct >= 1 && 'border-destructive/50 bg-destructive/5',
                        pct >= 0.8 && pct < 1 && 'border-amber-500/40 bg-amber-500/5',
                      )}
                    >
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          disabled={!toolPolicy || serverMode === 'all'}
                          checked={checked}
                          onChange={() => toggleServer(s.id)}
                          aria-label={`Allow ${s.name}`}
                        />
                        <span className="min-w-0 flex-1 truncate text-sm font-medium">
                          {s.name}
                        </span>
                        <Badge variant="secondary">{s.transport || 'http'}</Badge>
                      </div>
                      <TokenCapField
                        id={`cap-server-${s.id}`}
                        value={budgets[s.id] ?? ''}
                        used={tokens}
                        disabled={!toolPolicy}
                        onChange={(v) =>
                          setBudgets((prev) => ({ ...prev, [s.id]: v }))
                        }
                      />
                    </li>
                  )
                })}
              </ul>
              <Button disabled={!toolPolicy || saving} onClick={() => void saveServers()}>
                {saving ? 'Saving…' : 'Save server access'}
              </Button>
            </div>
          )}

          {tab === 'providers' && (
            <div className="space-y-4">
              {!toolPolicy && (
                <p className="text-xs text-muted-foreground">
                  Provider allowlists are a Pro feature — viewing usage is free.
                </p>
              )}
              <p className="text-xs text-muted-foreground">
                Optional per-provider token caps for this key. Empty = unlimited; over
                the cap the next request returns 402.
              </p>
              <div className="flex gap-2">
                {(['all', 'custom'] as const).map((m) => (
                  <button
                    key={m}
                    type="button"
                    disabled={!toolPolicy}
                    className={cn(
                      'rounded-md border px-3 py-1.5 text-xs font-medium',
                      providerMode === m
                        ? 'border-foreground bg-secondary'
                        : 'border-border text-muted-foreground',
                    )}
                    onClick={() => setProviderMode(m)}
                  >
                    {m === 'all' ? 'All providers' : 'Custom'}
                  </button>
                ))}
              </div>
              {providers.length === 0 && (
                <p className="text-sm text-muted-foreground">
                  No LLM providers configured yet.
                </p>
              )}
              <ul className="space-y-2">
                {providers.map((p) => {
                  const checked = providerMode === 'all' || selectedProviders.has(p.id)
                  const used = providerUsage.find((u) => u.provider_id === p.id)
                  const budgetN = Number(providerBudgets[p.id] || 0)
                  const tokens = used?.tokens ?? 0
                  const pct = budgetN > 0 ? tokens / budgetN : 0
                  return (
                    <li
                      key={p.id}
                      className={cn(
                        'rounded-md border border-border px-3 py-2',
                        pct >= 1 && 'border-destructive/50 bg-destructive/5',
                        pct >= 0.8 && pct < 1 && 'border-amber-500/40 bg-amber-500/5',
                      )}
                    >
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          disabled={!toolPolicy || providerMode === 'all'}
                          checked={checked}
                          onChange={() => toggleProvider(p.id)}
                          aria-label={`Allow ${p.name}`}
                        />
                        <span className="min-w-0 flex-1 truncate text-sm font-medium">
                          {p.name}
                        </span>
                      </div>
                      <TokenCapField
                        id={`cap-provider-${p.id}`}
                        value={providerBudgets[p.id] ?? ''}
                        used={tokens}
                        disabled={!toolPolicy}
                        onChange={(v) =>
                          setProviderBudgets((prev) => ({ ...prev, [p.id]: v }))
                        }
                      />
                    </li>
                  )
                })}
              </ul>
              <Button disabled={!toolPolicy || saving} onClick={() => void saveProviders()}>
                {saving ? 'Saving…' : 'Save provider access'}
              </Button>
            </div>
          )}

          {tab === 'tools' && (
            <div className="space-y-4">
              <p className="text-xs text-muted-foreground">
                Tool grants match by tool name across this key&apos;s servers (no
                per-server×tool matrix in v1).
              </p>
              <div className="flex gap-2">
                {(['all', 'custom'] as const).map((m) => (
                  <button
                    key={m}
                    type="button"
                    disabled={!toolPolicy}
                    className={cn(
                      'rounded-md border px-3 py-1.5 text-xs font-medium',
                      toolMode === m
                        ? 'border-foreground bg-secondary'
                        : 'border-border text-muted-foreground',
                    )}
                    onClick={() => setToolMode(m)}
                  >
                    {m === 'all' ? 'All tools' : 'Custom'}
                  </button>
                ))}
              </div>
              {toolMode === 'custom' && (
                <>
                  <select
                    className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm"
                    value={toolServerId}
                    onChange={(e) => setToolServerId(e.target.value)}
                  >
                    {servers.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                  <Input
                    placeholder="Search tools…"
                    value={toolSearch}
                    onChange={(e) => setToolSearch(e.target.value)}
                  />
                  <ul className="max-h-72 space-y-1 overflow-y-auto">
                    {filteredTools.map((t) => (
                      <li key={t.name} className="flex items-center gap-2 text-sm">
                        <input
                          type="checkbox"
                          disabled={!toolPolicy}
                          checked={allowedTools.has(t.name)}
                          onChange={() => toggleTool(t.name)}
                        />
                        <span className="font-mono text-xs">{t.name}</span>
                      </li>
                    ))}
                  </ul>
                </>
              )}
              <Button disabled={!toolPolicy || saving} onClick={() => void saveTools()}>
                {saving ? 'Saving…' : 'Save tool access'}
              </Button>
            </div>
          )}

          {tab === 'usage' && (
            <div className="space-y-4">
              <div className="space-y-2">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  MCP servers
                </h3>
                {usage.length === 0 && (
                  <p className="text-sm text-muted-foreground">No server usage this month.</p>
                )}
                {usage.map((u) => {
                  const pct =
                    u.monthly_budget > 0
                      ? Math.min(100, (u.tokens / u.monthly_budget) * 100)
                      : 0
                  return (
                    <div
                      key={u.server_id}
                      className="rounded-md border border-border px-3 py-2"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-medium">{u.name}</span>
                        <span className="tabular-nums text-xs text-muted-foreground">
                          {formatTokens(u.tokens)} · {u.requests} req
                        </span>
                      </div>
                      {u.monthly_budget > 0 && (
                        <div className="mt-2">
                          <div className="mb-1 flex justify-between text-[11px] text-muted-foreground">
                            <span>
                              Cap {formatTokens(u.tokens)} /{' '}
                              {formatTokens(u.monthly_budget)}
                            </span>
                            <span>{pct.toFixed(0)}%</span>
                          </div>
                          <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
                            <div
                              className={cn(
                                'h-full rounded-full bg-foreground/70',
                                pct >= 100 && 'bg-destructive',
                                pct >= 80 && pct < 100 && 'bg-amber-500',
                              )}
                              style={{ width: `${pct}%` }}
                            />
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
              <div className="space-y-2">
                <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  LLM providers
                </h3>
                {providerUsage.length === 0 && (
                  <p className="text-sm text-muted-foreground">No LLM usage this month.</p>
                )}
                {providerUsage.map((u) => {
                  const pct =
                    u.monthly_budget > 0
                      ? Math.min(100, (u.tokens / u.monthly_budget) * 100)
                      : 0
                  return (
                    <div
                      key={u.provider_id}
                      className="rounded-md border border-border px-3 py-2"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-medium">{u.name}</span>
                        <span className="tabular-nums text-xs text-muted-foreground">
                          {formatTokens(u.tokens)} · {u.requests} req
                        </span>
                      </div>
                      {u.monthly_budget > 0 && (
                        <div className="mt-2">
                          <div className="mb-1 flex justify-between text-[11px] text-muted-foreground">
                            <span>
                              {formatTokens(u.tokens)} of {formatTokens(u.monthly_budget)} on{' '}
                              {u.name}
                            </span>
                            <span>{pct.toFixed(0)}%</span>
                          </div>
                          <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
                            <div
                              className={cn(
                                'h-full rounded-full bg-foreground/70',
                                pct >= 100 && 'bg-destructive',
                                pct >= 80 && pct < 100 && 'bg-amber-500',
                              )}
                              style={{ width: `${pct}%` }}
                            />
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </div>
      </aside>
    </div>
  )
}

/** Per-server / per-provider monthly token kill-switch control. */
function TokenCapField({
  id,
  value,
  used,
  disabled,
  onChange,
}: {
  id: string
  value: string
  used: number
  disabled?: boolean
  onChange: (value: string) => void
}) {
  const cap = Number(value || 0)
  const pct = cap > 0 ? used / cap : 0
  const presets = [
    { label: 'Unlimited', tokens: 0 },
    { label: '1M', tokens: 1_000_000 },
    { label: '5M', tokens: 5_000_000 },
    { label: '10M', tokens: 10_000_000 },
    { label: '50M', tokens: 50_000_000 },
  ] as const
  const activePreset = presets.find((p) => p.tokens === cap)

  return (
    <div className="mt-2.5 space-y-2 border-t border-border/60 pt-2.5">
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={id} className="text-xs font-medium text-foreground">
          Monthly token cap
        </label>
        <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
          {cap > 0
            ? `${formatTokens(used)} / ${formatTokens(cap)}`
            : used > 0
              ? `${formatTokens(used)} used · unlimited`
              : 'Unlimited'}
        </span>
      </div>

      <div
        className={cn(
          'flex flex-wrap gap-1',
          disabled && 'pointer-events-none opacity-50',
        )}
        role="group"
        aria-label="Token cap presets"
      >
        {presets.map((p) => {
          const selected =
            p.tokens === 0 ? cap === 0 : activePreset?.tokens === p.tokens
          return (
            <button
              key={p.label}
              type="button"
              disabled={disabled}
              onClick={() => onChange(p.tokens === 0 ? '' : String(p.tokens))}
              className={cn(
                'rounded-md border px-2 py-1 text-[11px] font-medium tabular-nums transition-colors',
                selected
                  ? 'border-foreground bg-secondary text-foreground'
                  : 'border-border text-muted-foreground hover:border-foreground/40 hover:text-foreground',
              )}
            >
              {p.label}
            </button>
          )
        })}
      </div>

      <div
        className={cn(
          'flex h-9 max-w-[220px] items-center overflow-hidden rounded-md border border-border bg-background focus-within:ring-2 focus-within:ring-ring',
          disabled && 'opacity-50',
        )}
      >
        <input
          id={id}
          type="text"
          inputMode="numeric"
          disabled={disabled}
          placeholder="Custom"
          value={value}
          onChange={(e) => onChange(e.target.value.replace(/[^\d]/g, ''))}
          onBlur={() => {
            // Normalize shorthand typed into the field (e.g. keep digits-only value).
            if (value && Number(value) === 0) onChange('')
          }}
          className="h-full min-w-0 flex-1 bg-transparent px-3 font-mono text-xs tabular-nums outline-none placeholder:text-muted-foreground/70 disabled:cursor-not-allowed"
          aria-label="Custom monthly token cap"
        />
        <span className="shrink-0 border-l border-border bg-secondary/40 px-2.5 text-[11px] text-muted-foreground">
          tokens/mo
        </span>
      </div>

      {cap > 0 ? (
        <div className="h-1.5 overflow-hidden rounded-full bg-secondary">
          <div
            className={cn(
              'h-full rounded-full bg-foreground/70 transition-all duration-200',
              pct >= 1 && 'bg-destructive',
              pct >= 0.8 && pct < 1 && 'bg-amber-500',
            )}
            style={{ width: `${Math.min(100, pct * 100)}%` }}
          />
        </div>
      ) : null}
    </div>
  )
}
