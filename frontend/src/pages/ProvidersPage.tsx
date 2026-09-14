import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type LLMModel, type LLMProvider } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeader,
  TableLoading,
  TableRow,
} from '@/components/ui/table'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { useToast } from '@/components/Toast'
import { CopyButton } from '@/components/CopyButton'
import { cn } from '@/lib/utils'
import {
  matchProviderPreset,
  PROVIDER_PRESETS,
  shortModelAlias,
  suggestRouteAliases,
  type ProviderPreset,
} from '@/data/catalog'
import { Activity, Cpu, Plus, Power, Trash2 } from 'lucide-react'

function SuggestionChips({
  label,
  options,
  value,
  values,
  onPick,
  onToggle,
}: {
  label: string
  options: string[]
  value?: string
  values?: string[]
  onPick?: (v: string) => void
  onToggle?: (v: string) => void
}) {
  if (options.length === 0) return null
  const multi = !!onToggle
  return (
    <div className="space-y-1.5">
      <p className="text-[11px] font-medium text-muted-foreground">{label}</p>
      <div className="flex flex-wrap gap-1.5">
        {options.map((opt) => {
          const active = multi
            ? (values || []).includes(opt)
            : value === opt
          return (
            <button
              key={opt}
              type="button"
              onClick={() => (multi ? onToggle?.(opt) : onPick?.(opt))}
              aria-pressed={active}
              className={cn(
                'rounded-md border px-2 py-1 font-mono text-[11px] transition-colors',
                active
                  ? 'border-primary bg-primary/10 text-foreground'
                  : 'border-border bg-secondary/40 text-muted-foreground hover:border-foreground/30 hover:text-foreground',
              )}
            >
              {opt}
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function ProvidersPage() {
  const { t } = useTranslation()
  const { push } = useToast()
  const [providers, setProviders] = useState<LLMProvider[]>([])
  const [models, setModels] = useState<LLMModel[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [isAdmin, setIsAdmin] = useState(true)
  const [providerModal, setProviderModal] = useState(false)
  const [providerTab, setProviderTab] = useState<'quick' | 'custom'>('quick')
  const [modelModal, setModelModal] = useState(false)
  const [snippetModel, setSnippetModel] = useState<LLMModel | null>(null)
  const [healthMap, setHealthMap] = useState<
    Record<string, { ok: boolean; latency_ms: number; error: string }>
  >({})
  const [checking, setChecking] = useState<string | null>(null)
  const [addingId, setAddingId] = useState<string | null>(null)

  const [pName, setPName] = useState('')
  const [pBase, setPBase] = useState('https://api.openai.com/v1')
  const [pAuth, setPAuth] = useState('')
  const [pDefault, setPDefault] = useState('')
  const [pKeyLabel, setPKeyLabel] = useState('')
  const [pKeyUrl, setPKeyUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const authRef = useRef<HTMLInputElement>(null)

  const [mAliases, setMAliases] = useState<string[]>([])
  const [mCustomAlias, setMCustomAlias] = useState('')
  const [mModel, setMModel] = useState('')
  const [mProvider, setMProvider] = useState('')
  const [mFallback, setMFallback] = useState('')
  const [aliasTouched, setAliasTouched] = useState(false)

  const selectedProvider = providers.find((p) => p.id === mProvider)
  const matchedPreset = selectedProvider
    ? matchProviderPreset(selectedProvider)
    : undefined
  const upstreamSuggestions = useMemo(() => {
    const list: string[] = []
    const add = (m: string) => {
      if (m && !list.includes(m)) list.push(m)
    }
    if (selectedProvider?.default_model) add(selectedProvider.default_model)
    for (const m of matchedPreset?.models || []) add(m)
    if (matchedPreset?.default_model) add(matchedPreset.default_model)
    return list.slice(0, 8)
  }, [selectedProvider, matchedPreset])

  const aliasSuggestions = useMemo(
    () =>
      suggestRouteAliases({
        providerName: selectedProvider?.name || '',
        upstreamModel: mModel,
        existingAliases: [
          ...models.map((m) => m.name),
          // Don't hide chips already selected in this dialog
        ],
      }),
    [selectedProvider?.name, mModel, models],
  )

  const setDefaultAliases = (provName: string, upstream: string) => {
    const aliases = suggestRouteAliases({
      providerName: provName,
      upstreamModel: upstream,
      existingAliases: models.map((m) => m.name),
    })
    const short = shortModelAlias(upstream)
    const pick =
      aliases.find((a) => a === short) ||
      aliases.find((a) => a === 'chat') ||
      aliases[0]
    setMAliases(pick ? [pick] : [])
  }

  const toggleAlias = (alias: string) => {
    setAliasTouched(true)
    setMAliases((prev) =>
      prev.includes(alias) ? prev.filter((a) => a !== alias) : [...prev, alias],
    )
  }

  const addCustomAlias = () => {
    const t = mCustomAlias
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '')
    if (!t) return
    setAliasTouched(true)
    setMAliases((prev) => (prev.includes(t) ? prev : [...prev, t]))
    setMCustomAlias('')
  }

  const openModelModal = (providerId?: string) => {
    const pid = providerId || providers[0]?.id || ''
    const prov = providers.find((p) => p.id === pid)
    const preset = prov ? matchProviderPreset(prov) : undefined
    const upstream = prov?.default_model || preset?.default_model || ''
    setMProvider(pid)
    setMModel(upstream)
    setMFallback('')
    setAliasTouched(false)
    setMCustomAlias('')
    setDefaultAliases(prov?.name || '', upstream)
    setModelModal(true)
  }

  const onProviderChange = (id: string) => {
    setMProvider(id)
    const prov = providers.find((p) => p.id === id)
    const preset = prov ? matchProviderPreset(prov) : undefined
    const upstream = prov?.default_model || preset?.default_model || ''
    if (upstream) setMModel(upstream)
    if (!aliasTouched) setDefaultAliases(prov?.name || '', upstream)
  }

  const onUpstreamPick = (modelId: string) => {
    setMModel(modelId)
    if (!aliasTouched) setDefaultAliases(selectedProvider?.name || '', modelId)
  }

  const load = useCallback(async () => {
    setError('')
    try {
      const [plist, mlist, me] = await Promise.all([
        api.listLLMProviders(),
        api.listLLMModels(),
        api.me().catch(() => null),
      ])
      if (me) setIsAdmin(me.role === 'admin')
      setProviders(plist)
      setModels(mlist)
      if (plist.length && !mProvider) setMProvider(plist[0].id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load providers')
    } finally {
      setLoading(false)
    }
  }, [mProvider])

  useEffect(() => {
    void load()
  }, [load])

  const checkHealth = async (id: string) => {
    setChecking(id)
    try {
      const h = await api.healthLLMProvider(id)
      setHealthMap((prev) => ({ ...prev, [id]: h }))
      await load()
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Health check failed',
        tone: 'warning',
      })
    } finally {
      setChecking(null)
    }
  }

  const resetProviderForm = () => {
    setPName('')
    setPBase('https://api.openai.com/v1')
    setPAuth('')
    setPDefault('')
    setPKeyLabel('')
    setPKeyUrl('')
  }

  const applyProviderPreset = (preset: ProviderPreset, focusKey = true) => {
    setPName(preset.name)
    setPBase(preset.base_url)
    setPDefault(preset.default_model || '')
    setPAuth('')
    setPKeyLabel(preset.local ? '' : preset.key_label)
    setPKeyUrl(preset.local ? '' : preset.key_url)
    setProviderTab('custom')
    setProviderModal(true)
    if (focusKey && !preset.local) {
      window.setTimeout(() => authRef.current?.focus(), 50)
    }
  }

  const createProviderFromFields = async () => {
    await api.createLLMProvider({
      name: pName.trim(),
      base_url: pBase.trim(),
      auth_value: pAuth ? `Bearer ${pAuth.replace(/^Bearer\s+/i, '')}` : '',
      default_model: pDefault.trim(),
    })
  }

  const createProvider = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      await createProviderFromFields()
      push({ title: 'Provider added', tone: 'success' })
      setProviderModal(false)
      resetProviderForm()
      await load()
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Create failed',
        tone: 'warning',
      })
    } finally {
      setSaving(false)
    }
  }

  const addProviderNow = async (preset: ProviderPreset) => {
    if (!preset.local) {
      applyProviderPreset(preset)
      return
    }
    setAddingId(preset.id)
    try {
      await api.createLLMProvider({
        name: preset.name,
        base_url: preset.base_url,
        auth_value: '',
        default_model: preset.default_model || '',
      })
      push({ title: `${preset.name} added`, tone: 'success' })
      setProviderModal(false)
      resetProviderForm()
      await load()
    } catch (err) {
      push({
        title: err instanceof Error ? err.message : 'Create failed',
        tone: 'warning',
      })
      applyProviderPreset(preset, false)
    } finally {
      setAddingId(null)
    }
  }

  const createModel = async (e: FormEvent) => {
    e.preventDefault()
    const names = [...new Set(mAliases.map((a) => a.trim()).filter(Boolean))]
    if (names.length === 0 || !mModel.trim() || !mProvider) return
    setSaving(true)
    try {
      let created = 0
      const errors: string[] = []
      for (const name of names) {
        try {
          await api.createLLMModel({
            name,
            model: mModel.trim(),
            provider_id: mProvider,
            fallback_model_id: mFallback || null,
          })
          created++
        } catch (err) {
          errors.push(
            `${name}: ${err instanceof Error ? err.message : 'failed'}`,
          )
        }
      }
      if (created > 0) {
        push({
          title:
            created === 1
              ? 'Route alias created'
              : `${created} route aliases created`,
          tone: 'success',
        })
      }
      if (errors.length) {
        push({
          title: errors[0],
          tone: 'warning',
        })
      }
      if (created > 0 && errors.length === 0) {
        setModelModal(false)
        setAliasTouched(false)
        setMAliases([])
        setMCustomAlias('')
        setMModel('')
        setMFallback('')
      }
      await load()
    } finally {
      setSaving(false)
    }
  }

  const origin =
    typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8080'

  const sdkSnippet = (alias: string) =>
    `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${origin}/v1",
  apiKey: process.env.TCP_KEY, // tcp_*
});

const res = await client.chat.completions.create({
  model: "${alias}",
  messages: [{ role: "user", content: "Hello" }],
});`

  const curlSnippet = (alias: string) =>
    `curl ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer $TCP_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${alias}","messages":[{"role":"user","content":"Hello"}]}'`

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('providers.title')}
        description={t('providers.description')}
        actions={
          isAdmin ? (
            <div className="flex gap-2">
              <Button size="sm" variant="secondary" onClick={() => openModelModal()}>
                {t('providers.addRoute')}
              </Button>
              <Button
                size="sm"
                onClick={() => {
                  resetProviderForm()
                  setProviderTab('quick')
                  setProviderModal(true)
                }}
              >
                <Plus className="mr-1.5 h-4 w-4" />
                {t('providers.addProvider')}
              </Button>
            </div>
          ) : undefined
        }
      />

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}

      <section className="space-y-3">
        <h2 className="text-sm font-semibold tracking-tight">Providers</h2>
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : providers.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border px-4 py-6">
            <p className="mb-4 text-center text-sm text-muted-foreground">
              Pick a provider — local ones need no key.
            </p>
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3" data-testid="provider-quick-add">
              {PROVIDER_PRESETS.slice(0, 6).map((preset) => (
                <button
                  key={preset.id}
                  type="button"
                  className="rounded-lg border border-border p-3 text-left transition-all duration-150 hover:border-primary/40 hover:bg-secondary/30"
                  onClick={() => void addProviderNow(preset)}
                  disabled={addingId === preset.id}
                  aria-label={`Add ${preset.name}`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-semibold">{preset.name}</span>
                    {preset.local && <Badge variant="secondary">local</Badge>}
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">{preset.tagline}</p>
                </button>
              ))}
            </div>
            {isAdmin && (
              <div className="mt-4 flex justify-center">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    resetProviderForm()
                    setProviderTab('quick')
                    setProviderModal(true)
                  }}
                >
                  Browse all / custom…
                </Button>
              </div>
            )}
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {providers.map((p) => {
              const h = healthMap[p.id]
              return (
                <div
                  key={p.id}
                  className="rounded-lg border border-border px-4 py-3 transition-colors hover:bg-secondary/40"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{p.name}</span>
                        {!p.enabled && <Badge variant="secondary">disabled</Badge>}
                        {h?.ok && <Badge variant="success">healthy</Badge>}
                        {h && !h.ok && <Badge variant="destructive">down</Badge>}
                        {p.last_health_error && !h && (
                          <Badge variant="warning" title={p.last_health_error}>
                            last check failed
                          </Badge>
                        )}
                      </div>
                      <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
                        {p.base_url}
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {p.model_count ?? 0} routes
                        {p.default_model ? ` · default ${p.default_model}` : ''}
                      </p>
                    </div>
                    {isAdmin && (
                      <div className="flex shrink-0 gap-1">
                        <Button
                          size="icon"
                          variant="ghost"
                          title={t('providers.healthCheck')}
                          disabled={checking === p.id}
                          onClick={() => void checkHealth(p.id)}
                        >
                          <Activity
                            className={cn('h-4 w-4', checking === p.id && 'animate-pulse')}
                          />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          title={p.enabled ? 'Disable' : 'Enable'}
                          onClick={() =>
                            void api
                              .patchLLMProvider(p.id, { enabled: !p.enabled })
                              .then(load)
                              .catch((err) =>
                                push({
                                  title: err instanceof Error ? err.message : 'Update failed',
                                  tone: 'warning',
                                }),
                              )
                          }
                        >
                          <Power className="h-4 w-4" />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          title={t('providers.delete')}
                          onClick={() => {
                            if (!confirm(`Delete provider ${p.name}?`)) return
                            void api
                              .deleteLLMProvider(p.id)
                              .then(load)
                              .catch((err) =>
                                push({
                                  title: err instanceof Error ? err.message : 'Delete failed',
                                  tone: 'warning',
                                }),
                              )
                          }}
                        >
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    )}
                  </div>
                  {h && (
                    <p className="mt-2 text-xs text-muted-foreground">
                      {h.ok
                        ? `${h.latency_ms} ms`
                        : h.error || 'unreachable'}
                    </p>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold tracking-tight">Model routes</h2>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Alias</TableHead>
              <TableHead>Upstream model</TableHead>
              <TableHead>Provider</TableHead>
              <TableHead>Fallback</TableHead>
              <TableHead className="w-[140px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && <TableLoading colSpan={5} rows={2} />}
            {!loading && models.length === 0 && (
              <TableEmpty
                colSpan={5}
                icon={Cpu}
                title={t('providers.noAliases')}
                description='Clients send model: "chat" — create an alias that rewrites to the upstream id.'
                actionLabel={isAdmin ? 'Add model route' : undefined}
                onAction={isAdmin ? () => openModelModal() : undefined}
              />
            )}
            {models.map((m) => (
              <TableRow key={m.id}>
                <TableCell className="font-mono text-sm">{m.name}</TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {m.model}
                </TableCell>
                <TableCell>{m.provider_name || m.provider_id}</TableCell>
                <TableCell>
                  {m.fallback_name ? (
                    <Badge variant="secondary">{m.fallback_name}</Badge>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </TableCell>
                <TableCell>
                  <div className="flex justify-end gap-1">
                    <Button size="sm" variant="secondary" onClick={() => setSnippetModel(m)}>
                      Copy config
                    </Button>
                    {isAdmin && (
                      <Button
                        size="icon"
                        variant="ghost"
                        onClick={() => {
                          if (!confirm(`Delete route ${m.name}?`)) return
                          void api.deleteLLMModel(m.id).then(load)
                        }}
                      >
                        <Trash2 className="h-4 w-4 text-destructive" />
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </section>

      <Dialog
        open={providerModal}
        onClose={() => setProviderModal(false)}
        title={t('providers.addProvider')}
        description="One-click presets or a custom OpenAI-compatible base URL"
        className="!max-w-2xl"
      >
        <div
          className="mb-4 grid grid-cols-2 gap-1 rounded-md border border-border p-1"
          role="tablist"
          aria-label="Provider add mode"
        >
          {(
            [
              ['quick', 'Quick add'],
              ['custom', 'Custom'],
            ] as const
          ).map(([tab, label]) => (
            <button
              key={tab}
              type="button"
              role="tab"
              aria-selected={providerTab === tab}
              className={cn(
                'rounded px-2 py-1.5 text-xs font-medium transition-colors',
                providerTab === tab
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )}
              onClick={() => setProviderTab(tab)}
            >
              {label}
            </button>
          ))}
        </div>

        {providerTab === 'quick' ? (
          <div className="max-h-[55vh] space-y-2 overflow-y-auto" data-testid="provider-preset-grid">
            {PROVIDER_PRESETS.map((preset) => (
              <div
                key={preset.id}
                className="flex items-center justify-between gap-3 rounded-lg border border-border p-3"
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left"
                  onClick={() => applyProviderPreset(preset)}
                  aria-label={`Use ${preset.name} preset`}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold">{preset.name}</span>
                    {preset.local && <Badge variant="secondary">local</Badge>}
                  </div>
                  <p className="mt-0.5 text-xs text-muted-foreground">{preset.tagline}</p>
                  <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
                    {preset.base_url}
                  </p>
                </button>
                {preset.local ? (
                  <Button
                    size="sm"
                    disabled={addingId === preset.id}
                    onClick={() => void addProviderNow(preset)}
                  >
                    {addingId === preset.id ? 'Adding…' : 'Add now'}
                  </Button>
                ) : (
                  <Button size="sm" variant="secondary" onClick={() => applyProviderPreset(preset)}>
                    Use
                  </Button>
                )}
              </div>
            ))}
          </div>
        ) : (
          <form className="space-y-4" onSubmit={(e) => void createProvider(e)}>
            <div className="space-y-2">
              <Label htmlFor="pname">Name</Label>
              <Input id="pname" value={pName} onChange={(e) => setPName(e.target.value)} required />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pbase">Base URL (ends at /v1)</Label>
              <Input id="pbase" value={pBase} onChange={(e) => setPBase(e.target.value)} required />
            </div>
            <div className="space-y-2">
              <Label htmlFor="pauth">{pKeyLabel || 'API key (optional)'}</Label>
              <Input
                id="pauth"
                ref={authRef}
                type="password"
                placeholder={pKeyLabel ? undefined : 'optional for local providers'}
                value={pAuth}
                onChange={(e) => setPAuth(e.target.value)}
              />
              {pKeyUrl && (
                <p className="text-[11px] text-muted-foreground">
                  <a
                    href={pKeyUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline underline-offset-2 hover:text-foreground"
                  >
                    Get {pKeyLabel || 'key'}
                  </a>
                </p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="pdef">Default model</Label>
              <Input id="pdef" value={pDefault} onChange={(e) => setPDefault(e.target.value)} />
            </div>
            <Button type="submit" disabled={saving}>
              {saving ? 'Saving…' : 'Create'}
            </Button>
          </form>
        )}
      </Dialog>

      <Dialog open={modelModal} onClose={() => setModelModal(false)} title={t('providers.addRoute')}>
        <form className="space-y-4" onSubmit={(e) => void createModel(e)}>
          <div className="space-y-2">
            <Label htmlFor="mprov">Provider</Label>
            <select
              id="mprov"
              className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm"
              value={mProvider}
              onChange={(e) => onProviderChange(e.target.value)}
              required
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="mmodel">Upstream model id</Label>
            <Input
              id="mmodel"
              value={mModel}
              onChange={(e) => {
                setMModel(e.target.value)
                if (!aliasTouched) {
                  setDefaultAliases(selectedProvider?.name || '', e.target.value)
                }
              }}
              placeholder={upstreamSuggestions[0] || 'gpt-4o-mini'}
              required
            />
            <SuggestionChips
              label={
                matchedPreset
                  ? `Popular on ${matchedPreset.name}`
                  : 'Suggestions'
              }
              options={upstreamSuggestions}
              value={mModel}
              onPick={onUpstreamPick}
            />
          </div>

          <div className="space-y-2">
            <Label>Route aliases</Label>
            <p className="text-xs text-muted-foreground">
              Clients put these in{' '}
              <code className="rounded bg-secondary px-1 py-0.5 text-[11px]">model</code>
              . Select several — each becomes its own route to the same upstream.
            </p>
            <SuggestionChips
              label="Toggle to select"
              options={aliasSuggestions}
              values={mAliases}
              onToggle={toggleAlias}
            />
            {mAliases.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {mAliases.map((a) => (
                  <span
                    key={a}
                    className="inline-flex items-center gap-1 rounded-md border border-primary/30 bg-primary/5 px-2 py-0.5 font-mono text-[11px]"
                  >
                    {a}
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground"
                      aria-label={`Remove ${a}`}
                      onClick={() => toggleAlias(a)}
                    >
                      ×
                    </button>
                  </span>
                ))}
              </div>
            )}
            <div className="flex gap-2">
              <Input
                id="mname"
                value={mCustomAlias}
                onChange={(e) => setMCustomAlias(e.target.value)}
                placeholder="custom-alias"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    addCustomAlias()
                  }
                }}
              />
              <Button
                type="button"
                variant="outline"
                disabled={!mCustomAlias.trim()}
                onClick={addCustomAlias}
              >
                Add
              </Button>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="mfb">Fallback route (optional)</Label>
            <select
              id="mfb"
              className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm"
              value={mFallback}
              onChange={(e) => setMFallback(e.target.value)}
            >
              <option value="">None</option>
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name} → {m.model}
                </option>
              ))}
            </select>
          </div>
          <Button
            type="submit"
            disabled={saving || !mProvider || mAliases.length === 0 || !mModel.trim()}
          >
            {saving
              ? 'Saving…'
              : mAliases.length <= 1
                ? 'Create route'
                : `Create ${mAliases.length} routes`}
          </Button>
        </form>
      </Dialog>

      <Dialog
        open={!!snippetModel}
        onClose={() => setSnippetModel(null)}
        title={snippetModel ? `Client config — ${snippetModel.name}` : 'Client config'}
      >
        {snippetModel && (
          <div className="space-y-4">
            <div>
              <div className="mb-1 flex items-center justify-between">
                <Label>OpenAI SDK</Label>
                <CopyButton text={sdkSnippet(snippetModel.name)} />
              </div>
              <pre className="overflow-x-auto rounded-md border border-border bg-secondary/40 p-3 text-xs">
                {sdkSnippet(snippetModel.name)}
              </pre>
            </div>
            <div>
              <div className="mb-1 flex items-center justify-between">
                <Label>curl</Label>
                <CopyButton text={curlSnippet(snippetModel.name)} />
              </div>
              <pre className="overflow-x-auto rounded-md border border-border bg-secondary/40 p-3 text-xs">
                {curlSnippet(snippetModel.name)}
              </pre>
            </div>
          </div>
        )}
      </Dialog>
    </div>
  )
}
