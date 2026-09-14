import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type CatalogTool, type CircuitStatus, type MCPServer, type ToolUsage } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Icon } from '@/components/ui/icon'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/Toast'
import { cn } from '@/lib/utils'
import { RefreshCw, X } from 'lucide-react'

interface Props {
  server: MCPServer
  toolPolicy: boolean
  onClose: () => void
  onRetest: (id: string) => void
}

export function ServerDetailDrawer({ server, toolPolicy, onClose, onRetest }: Props) {
  const { push } = useToast()
  const [tab, setTab] = useState<'tools' | 'usage' | 'process'>('tools')
  const [tools, setTools] = useState<CatalogTool[]>([])
  const [usage, setUsage] = useState<ToolUsage[]>([])
  const [proc, setProc] = useState<CircuitStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [hintShown, setHintShown] = useState(false)
  const [expanded, setExpanded] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [t, u, st] = await Promise.all([
        api.listServerTools(server.id),
        api.usageTools(server.id),
        api.serverStatus(server.id).catch(() => null),
      ])
      setTools(t)
      setUsage(u)
      setProc(st)
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Failed to load tools',
        tone: 'warning',
      })
    } finally {
      setLoading(false)
    }
  }, [server.id, push])

  useEffect(() => {
    void load()
  }, [load])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return tools
    return tools.filter(
      (t) =>
        t.name.toLowerCase().includes(q) ||
        (t.description || '').toLowerCase().includes(q),
    )
  }, [tools, search])

  const toggleSelect = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const selectAllMatching = () => {
    setSelected(new Set(filtered.map((t) => t.name)))
  }

  const onToggle = async (name: string, enabled: boolean) => {
    if (!toolPolicy) {
      if (!hintShown) {
        push({ title: 'Tool control is a Pro feature', tone: 'warning' })
        setHintShown(true)
      }
      return
    }
    try {
      await api.patchServerTool(server.id, name, enabled)
      setTools((prev) => prev.map((t) => (t.name === name ? { ...t, enabled } : t)))
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Update failed',
        tone: 'warning',
      })
    }
  }

  const bulk = async (enabled: boolean, scope: 'all' | 'selected') => {
    if (!toolPolicy) {
      push({ title: 'Tool control is a Pro feature', tone: 'warning' })
      return
    }
    try {
      await api.bulkServerTools(
        server.id,
        enabled,
        scope,
        scope === 'selected' ? [...selected] : undefined,
      )
      await load()
      push({ title: enabled ? 'Tools enabled' : 'Tools disabled', tone: 'success' })
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Bulk update failed',
        tone: 'warning',
      })
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/30" onClick={onClose}>
      <aside
        className="flex h-full w-full max-w-xl flex-col border-l border-border bg-background shadow-xl"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label={`${server.name} details`}
      >
        <div className="flex items-start justify-between border-b border-border px-5 py-4">
          <div>
            <h2 className="text-lg font-semibold tracking-tight">{server.name}</h2>
            <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
              {server.transport === 'stdio' ? server.command || 'stdio' : server.base_url}
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <Icon icon={X} className="h-4 w-4" />
          </Button>
        </div>

        <div className="flex gap-1 border-b border-border px-5 pt-2">
          {(
            (server.transport === 'stdio'
              ? (['tools', 'usage', 'process'] as const)
              : (['tools', 'usage'] as const))
          ).map((t) => (
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
          {tab === 'tools' && (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  placeholder="Search tools…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  className="max-w-xs"
                />
                <Button type="button" variant="outline" size="sm" onClick={selectAllMatching}>
                  Select matching
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={!toolPolicy}
                  onClick={() => void bulk(true, 'all')}
                >
                  Enable all
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={!toolPolicy}
                  onClick={() => void bulk(false, 'all')}
                >
                  Disable all
                </Button>
                {selected.size > 0 && (
                  <>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={!toolPolicy}
                      onClick={() => void bulk(true, 'selected')}
                    >
                      Enable selection
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={!toolPolicy}
                      onClick={() => void bulk(false, 'selected')}
                    >
                      Disable selection
                    </Button>
                  </>
                )}
              </div>
              {!toolPolicy && (
                <p className="text-xs text-muted-foreground">
                  Tool control is a Pro feature — viewing is free.
                </p>
              )}

              {loading && (
                <div className="space-y-2">
                  <Skeleton className="h-10 w-full" />
                  <Skeleton className="h-10 w-full" />
                  <Skeleton className="h-10 w-full" />
                </div>
              )}

              {!loading && tools.length === 0 && (
                <div className="rounded-md border border-dashed border-border p-6 text-center">
                  <p className="text-sm text-muted-foreground">Not indexed yet</p>
                  <Button
                    className="mt-3"
                    variant="outline"
                    size="sm"
                    onClick={() => onRetest(server.id)}
                  >
                    <Icon icon={RefreshCw} className="mr-1.5 h-3.5 w-3.5" />
                    {server.transport === 'stdio' ? 'Restart & reindex' : 'Reindex'}
                  </Button>
                </div>
              )}

              {!loading &&
                filtered.map((t) => (
                  <div
                    key={t.name}
                    className="flex items-start gap-3 rounded-md border border-border px-3 py-2"
                  >
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={selected.has(t.name)}
                      onChange={() => toggleSelect(t.name)}
                      aria-label={`Select ${t.name}`}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          className="truncate font-mono text-sm font-medium hover:underline"
                          onClick={() =>
                            setExpanded((e) => (e === t.name ? null : t.name))
                          }
                        >
                          {t.name}
                        </button>
                        {!t.enabled && <Badge variant="secondary">off</Badge>}
                      </div>
                      {t.description && (
                        <p
                          className={cn(
                            'mt-0.5 text-xs text-muted-foreground',
                            expanded !== t.name && 'line-clamp-1',
                          )}
                        >
                          {t.description}
                        </p>
                      )}
                    </div>
                    <span className="shrink-0 tabular-nums text-xs text-muted-foreground">
                      {t.calls_30d}
                    </span>
                    <Switch
                      checked={t.enabled}
                      disabled={!toolPolicy}
                      onCheckedChange={(v) => void onToggle(t.name, v)}
                      title={toolPolicy ? undefined : 'Tool control is a Pro feature'}
                    />
                  </div>
                ))}
            </div>
          )}

          {tab === 'usage' && (
            <div className="space-y-2">
              {usage.length === 0 && (
                <p className="text-sm text-muted-foreground">No tool calls yet for this server.</p>
              )}
              {usage.map((u) => (
                <div
                  key={u.name}
                  className="flex items-center justify-between rounded-md border border-border px-3 py-2"
                >
                  <span className="font-mono text-sm">{u.name}</span>
                  <span className="tabular-nums text-sm text-muted-foreground">
                    {u.call_count}
                  </span>
                </div>
              ))}
            </div>
          )}

          {tab === 'process' && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3 text-sm">
                <div>
                  <p className="text-xs text-muted-foreground">State</p>
                  <p className="font-medium">{proc?.process?.state || '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Uptime</p>
                  <p className="font-medium tabular-nums">
                    {proc?.process?.uptime_sec != null ? `${proc.process.uptime_sec}s` : '—'}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">In-flight</p>
                  <p className="font-medium tabular-nums">{proc?.process?.in_flight ?? 0}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Strikes</p>
                  <p className="font-medium tabular-nums">{proc?.process?.strikes ?? 0}</p>
                </div>
              </div>
              {proc?.process?.last_error && (
                <p className="text-xs text-destructive">{proc.process.last_error}</p>
              )}
              {proc?.process?.stderr_tail && proc.process.stderr_tail.length > 0 && (
                <div>
                  <p className="mb-1 text-xs text-muted-foreground">Stderr (tail)</p>
                  <pre className="max-h-40 overflow-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-[11px]">
                    {proc.process.stderr_tail.join('\n')}
                  </pre>
                </div>
              )}
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  onRetest(server.id)
                  void load()
                }}
              >
                <Icon icon={RefreshCw} className="mr-1.5 h-3.5 w-3.5" />
                Restart & reindex
              </Button>
            </div>
          )}
        </div>
      </aside>
    </div>
  )
}
