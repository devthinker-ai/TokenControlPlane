import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Loader2, Server } from 'lucide-react'
import {
  api,
  type ClientSnippets,
  type CreateKeyResponse,
  type CreateServerResponse,
  type IndexedTool,
} from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { KeyReveal } from '@/components/KeyReveal'
import { CopyButton } from '@/components/CopyButton'
import { GuidedServerEmpty } from '@/components/GuidedServerEmpty'
import { useToast } from '@/components/Toast'
import { cn } from '@/lib/utils'
import { canAutoAdd, type ServerPreset } from '@/data/catalog'
import { createInputFromPreset } from '@/lib/createInputFromPreset'
import { hintForError, hostFromUrl } from '@/lib/hintForError'

const CLIENTS = [
  {
    id: 'claude',
    label: 'Claude Desktop',
    where: 'Open Claude Desktop → Settings → Developer → Edit Config, then paste into claude_desktop_config.json.',
  },
  {
    id: 'cursor',
    label: 'Cursor',
    where: 'Cursor Settings → MCP → Add server (or edit ~/.cursor/mcp.json) and paste the JSON.',
  },
  {
    id: 'windsurf',
    label: 'Windsurf',
    where: 'Windsurf MCP settings → paste into the mcpServers config file.',
  },
  {
    id: 'other',
    label: 'Other client',
    where: 'Use the endpoint URL + Bearer header, or the generic mcpServers JSON most clients accept.',
  },
] as const

export interface WizardResult {
  server?: CreateServerResponse
  key?: CreateKeyResponse
}

interface SetupWizardProps {
  onDone: () => void
  onSkipComplete: () => void
}

export function SetupWizard({ onDone, onSkipComplete }: SetupWizardProps) {
  const { t } = useTranslation()
  const { push } = useToast()
  const [step, setStep] = useState(0)
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [authHeader, setAuthHeader] = useState('')
  const [authValue, setAuthValue] = useState('')
  const [indexing, setIndexing] = useState(false)
  const [indexError, setIndexError] = useState('')
  const [tools, setTools] = useState<IndexedTool[]>([])
  const [server, setServer] = useState<CreateServerResponse | null>(null)
  const [keyName, setKeyName] = useState('my-laptop')
  const [keyRes, setKeyRes] = useState<CreateKeyResponse | null>(null)
  const [creatingKey, setCreatingKey] = useState(false)
  const [client, setClient] = useState<(typeof CLIENTS)[number]['id']>('claude')
  const [finishing, setFinishing] = useState(false)
  const [addingId, setAddingId] = useState<string | null>(null)
  const [keyHint, setKeyHint] = useState('')
  const [keyUrl, setKeyUrl] = useState('')

  const gatewayBase =
    typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8080'

  const applyPreset = (preset: ServerPreset) => {
    setName(preset.name)
    setKeyHint(preset.auth.key_label || '')
    setKeyUrl(preset.auth.key_url || '')
    if (preset.transport === 'stdio') {
      if (canAutoAdd(preset)) {
        void addPresetNow(preset)
      } else {
        setIndexError(
          `${preset.name} needs local env (${(preset.env || []).map((e) => e.key).join(', ')}). Use Servers → Quick add to finish.`,
        )
      }
      return
    }
    setBaseUrl(preset.base_url || '')
    setAuthHeader(preset.auth.header || (preset.auth.type === 'static' ? 'Authorization' : ''))
    setAuthValue('')
  }

  const handleCreateResult = (res: CreateServerResponse, preset?: ServerPreset) => {
    setServer(res)
    setTools(res.tools || [])
    if (res.indexing_error) {
      const hint = hintForError(res.indexing_error, {
        keyLabel: preset?.auth.key_label || keyHint || undefined,
        command: preset?.command,
        host: hostFromUrl(preset?.base_url || baseUrl),
      })
      setIndexError(hint ? `${res.indexing_error}\n${hint}` : res.indexing_error)
    } else {
      setStep(2)
    }
  }

  const addPresetNow = async (preset: ServerPreset) => {
    if (!canAutoAdd(preset) && preset.transport === 'http') {
      applyPreset(preset)
      return
    }
    if (!canAutoAdd(preset) && preset.transport === 'stdio') {
      // Needs env — land on form with name filled; user can finish on Servers page.
      setName(preset.name)
      setIndexError(
        `${preset.name} needs local env (${(preset.env || []).map((e) => e.key).join(', ')}). Use Servers → Quick add to finish.`,
      )
      return
    }
    setAddingId(preset.id)
    setIndexError('')
    setIndexing(true)
    try {
      const res = await api.createServer(createInputFromPreset(preset))
      handleCreateResult(res, preset)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to add server'
      console.error(err)
      setIndexError(msg)
    } finally {
      setIndexing(false)
      setAddingId(null)
    }
  }

  const submitServer = async (e?: FormEvent) => {
    e?.preventDefault()
    setIndexing(true)
    setIndexError('')
    try {
      const res = await api.createServer({
        name,
        base_url: baseUrl,
        auth_header: authHeader || undefined,
        auth_value: authValue || undefined,
      })
      handleCreateResult(res)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to add server'
      console.error(err)
      const hint = hintForError(msg, {
        keyLabel: keyHint || undefined,
        host: hostFromUrl(baseUrl),
      })
      setIndexError(hint ? `${msg}\n${hint}` : msg)
    } finally {
      setIndexing(false)
    }
  }

  const retryIndex = async () => {
    if (!server) {
      await submitServer()
      return
    }
    setIndexing(true)
    setIndexError('')
    try {
      const res = await api.reindexServer(server.id)
      setTools(res.tools || [])
      if (res.indexing_error) {
        setIndexError(res.indexing_error)
      } else {
        setServer({ ...server, ...res, tools: res.tools })
        setStep(2)
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Reindex failed'
      console.error(err)
      setIndexError(msg)
    } finally {
      setIndexing(false)
    }
  }

  const createKey = async (e: FormEvent) => {
    e.preventDefault()
    setCreatingKey(true)
    try {
      const res = await api.createKey(keyName, server?.id)
      setKeyRes(res)
      setStep(3)
    } catch (err) {
      console.error(err)
      push({
        title: t('wizard.toastKeyFail'),
        description: err instanceof Error ? err.message : t('wizard.toastTryAgain'),
        tone: 'danger',
      })
    } finally {
      setCreatingKey(false)
    }
  }

  const finish = async () => {
    setFinishing(true)
    try {
      await api.completeOnboarding()
      onDone()
    } catch (err) {
      console.error(err)
      push({
        title: t('wizard.toastOnboardFail'),
        description: err instanceof Error ? err.message : t('wizard.toastTryAgain'),
        tone: 'danger',
      })
    } finally {
      setFinishing(false)
    }
  }

  const skip = () => {
    push({
      title: t('wizard.toastSkipped'),
      description: t('wizard.toastSkippedDesc'),
      tone: 'warning',
    })
    void api.completeOnboarding().then(onSkipComplete).catch((err) => {
      console.error(err)
      onSkipComplete()
    })
  }

  const snippetFor = (id: string, snippets: ClientSnippets | undefined) => {
    if (!snippets) return ''
    if (id === 'claude') return snippets.claude_desktop
    if (id === 'cursor') return snippets.cursor
    if (id === 'windsurf') return snippets.windsurf
    return snippets.claude_desktop
  }

  const endpointURL = server
    ? `${gatewayBase}/mcp/${server.id}`
    : `${gatewayBase}/mcp/<server_id>`
  const authLine = keyRes ? `Authorization: Bearer ${keyRes.key}` : 'Authorization: Bearer <key>'

  return (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-background/90 p-4 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      aria-labelledby="wizard-title"
    >
      <div className="flex max-h-[90vh] w-full max-w-xl flex-col overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
        <div className="border-b border-border px-6 py-4">
          <div className="flex items-center justify-between gap-3">
            <h2 id="wizard-title" className="text-lg font-semibold tracking-tight">
              Setup
            </h2>
            <Button type="button" variant="ghost" size="sm" onClick={skip} aria-label={t('wizard.skipAria')}>
              Skip
            </Button>
          </div>
          <div className="mt-4 flex items-center gap-1" aria-hidden>
            {[0, 1, 2, 3, 4].map((i) => (
              <div key={i} className="flex flex-1 items-center gap-1 last:flex-none">
                <span
                  className={cn(
                    'flex h-6 w-6 items-center justify-center rounded-full text-[11px] font-medium tabular-nums',
                    i < step && 'bg-primary text-primary-foreground',
                    i === step && 'bg-primary text-primary-foreground',
                    i > step && 'bg-secondary text-muted-foreground',
                  )}
                >
                  {i < step ? <Check className="h-3 w-3" strokeWidth={2} /> : i + 1}
                </span>
                {i < 4 && (
                  <span
                    className={cn(
                      'h-px flex-1',
                      i < step ? 'bg-primary' : 'bg-border',
                    )}
                  />
                )}
              </div>
            ))}
          </div>
          <p className="mt-2 text-xs text-muted-foreground">{t('wizard.stepOf', { current: step + 1, total: 5 })}</p>
        </div>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          {step === 0 && (
            <div className="space-y-5">
              <div>
                <h3 className="text-xl font-semibold">{t('wizard.welcomeTitle')}</h3>
                <p className="mt-2 text-sm text-muted-foreground">
                  {t('wizard.welcomeBody')}
                </p>
              </div>
              <ul className="space-y-3 text-sm">
                <li className="flex gap-2">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                  {t('wizard.bullet1')}
                </li>
                <li className="flex gap-2">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                  {t('wizard.bullet2')}
                </li>
                <li className="flex gap-2">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                  {t('wizard.bullet3')}
                </li>
              </ul>
              <Button className="w-full" onClick={() => setStep(1)} aria-label={t('wizard.setupFirst')}>
                {t('wizard.setupFirst')}
              </Button>
            </div>
          )}

          {step === 1 && (
            <form onSubmit={(e) => void submitServer(e)} className="space-y-4">
              <div>
                <h3 className="text-lg font-semibold">{t('wizard.addFirstTitle')}</h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  {t('wizard.addFirstBody')}
                </p>
              </div>
              <GuidedServerEmpty
                embedded
                hideBrowse
                className="px-0 py-2"
                onSelect={(p) => {
                  if (canAutoAdd(p)) {
                    void addPresetNow(p)
                  } else {
                    applyPreset(p)
                  }
                }}
                onAddNow={(p) => void addPresetNow(p)}
                addingId={addingId}
              />
              {keyHint && (
                <p className="text-xs text-amber-700 dark:text-amber-400">
                  Needs {keyHint}
                  {keyUrl ? (
                    <>
                      {' — '}
                      <a
                        href={keyUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="underline underline-offset-2"
                      >
                        get key
                      </a>
                    </>
                  ) : null}
                </p>
              )}
              <div className="space-y-2">
                <Label htmlFor="wiz-name">{t('wizard.name')}</Label>
                <Input id="wiz-name" value={name} onChange={(e) => setName(e.target.value)} required />
              </div>
              <div className="space-y-2">
                <Label htmlFor="wiz-url">{t('wizard.url')}</Label>
                <Input
                  id="wiz-url"
                  type="url"
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  required
                />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="wiz-ah">Auth header</Label>
                  <Input
                    id="wiz-ah"
                    placeholder="Authorization"
                    value={authHeader}
                    onChange={(e) => setAuthHeader(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="wiz-av">Auth value</Label>
                  <Input
                    id="wiz-av"
                    type="password"
                    placeholder="Bearer …"
                    value={authValue}
                    onChange={(e) => setAuthValue(e.target.value)}
                  />
                </div>
              </div>
              {indexing && (
                <p className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Contacting server &amp; reading its tools…
                </p>
              )}
              {indexError && (
                <div className="rounded-md border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-200">
                  <p className="whitespace-pre-line">{indexError}</p>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="mt-2"
                    onClick={() => void retryIndex()}
                    disabled={indexing}
                  >
                    Retry
                  </Button>
                </div>
              )}
              {!indexError && tools.length > 0 && (
                <ul className="space-y-1 rounded-md border border-border p-3 text-sm">
                  {tools.map((t) => (
                    <li key={t.name} className="flex items-center gap-2">
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                      {t.name}
                    </li>
                  ))}
                </ul>
              )}
              <div className="flex justify-between gap-2 pt-2">
                <Button type="button" variant="outline" onClick={() => setStep(0)}>
                  Back
                </Button>
                <Button type="submit" disabled={indexing || !name || !baseUrl}>
                  {indexing ? t('common.loading') : t('wizard.addServer')}
                </Button>
              </div>
            </form>
          )}

          {step === 2 && (
            <form onSubmit={(e) => void createKey(e)} className="space-y-4">
              <div>
                <h3 className="text-lg font-semibold">{t('wizard.createKeyTitle')}</h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  Machine key for Cursor / Claude Desktop / Windsurf.
                </p>
              </div>
              {tools.length > 0 && (
                <ul className="max-h-32 space-y-1 overflow-y-auto rounded-md border border-border p-3 text-sm">
                  {tools.map((t) => (
                    <li key={t.name} className="flex items-center gap-2">
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                      {t.name}
                    </li>
                  ))}
                </ul>
              )}
              <div className="space-y-2">
                <Label htmlFor="wiz-key">Key name</Label>
                <Input
                  id="wiz-key"
                  value={keyName}
                  onChange={(e) => setKeyName(e.target.value)}
                  required
                />
              </div>
              <div className="flex justify-between gap-2">
                <Button type="button" variant="outline" onClick={() => setStep(1)}>
                  Back
                </Button>
                <Button type="submit" disabled={creatingKey}>
                  {creatingKey ? t('common.loading') : t('wizard.generateKey')}
                </Button>
              </div>
            </form>
          )}

          {step === 3 && keyRes && (
            <div className="space-y-4">
              <div>
                <h3 className="text-lg font-semibold">{t('wizard.pickClient')}</h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  Copy the config for your AI client — or use Other for any streamable-HTTP client.
                </p>
              </div>
              <KeyReveal plaintextKey={keyRes.key} />
              <div className="grid gap-2 sm:grid-cols-2">
                {CLIENTS.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    onClick={() => setClient(c.id)}
                    className={cn(
                      'rounded-lg border px-3 py-3 text-left text-sm transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                      client === c.id
                        ? 'border-primary bg-primary/10'
                        : 'border-border hover:border-muted-foreground/40',
                    )}
                    aria-pressed={client === c.id}
                    aria-label={c.label}
                  >
                    <span className="font-medium">{c.label}</span>
                  </button>
                ))}
              </div>

              {client !== 'other' ? (
                <div className="space-y-2">
                  <pre className="max-h-48 overflow-auto rounded-md border border-border bg-background p-3 text-xs">
                    {snippetFor(client, keyRes.snippets)}
                  </pre>
                  <CopyButton text={snippetFor(client, keyRes.snippets)} label="Copy config" />
                  <p className="text-xs text-muted-foreground">
                    {CLIENTS.find((c) => c.id === client)?.where}
                  </p>
                </div>
              ) : (
                <div className="space-y-3 rounded-lg border border-border p-3">
                  <div>
                    <p className="text-xs font-medium text-muted-foreground">Endpoint URL</p>
                    <code className="mt-1 block break-all text-xs">{endpointURL}</code>
                    <CopyButton className="mt-2" text={endpointURL} label="Copy URL" />
                  </div>
                  <div>
                    <p className="text-xs font-medium text-muted-foreground">Auth header</p>
                    <code className="mt-1 block break-all text-xs">{authLine}</code>
                    <CopyButton className="mt-2" text={authLine} label="Copy header" />
                  </div>
                  <div>
                    <p className="text-xs font-medium text-muted-foreground">Generic mcpServers JSON</p>
                    <pre className="mt-1 max-h-40 overflow-auto rounded-md bg-background p-2 text-xs">
                      {keyRes.snippets.claude_desktop}
                    </pre>
                    <CopyButton
                      className="mt-2"
                      text={keyRes.snippets.claude_desktop}
                      label="Copy JSON"
                    />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    The gateway is client-agnostic — any MCP client with streamable-HTTP + custom
                    header support works (VS Code/Cline/Roo, Zed, Cherry Studio, OpenWebUI,
                    Continue, Claude Code CLI, etc.).
                  </p>
                </div>
              )}

              <div className="flex justify-between gap-2">
                <Button type="button" variant="outline" onClick={() => setStep(2)}>
                  Back
                </Button>
                <Button type="button" onClick={() => setStep(4)}>
                  Next
                </Button>
              </div>
            </div>
          )}

          {step === 4 && (
            <div className="space-y-5">
              <div className="wizard-success mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-emerald-500/15">
                <Check className="h-7 w-7 text-emerald-400" />
              </div>
              <div className="text-center">
                <h3 className="text-xl font-semibold">You&apos;re set</h3>
                <p className="mt-2 text-sm text-muted-foreground">
                  Teammates connect with their own key to the same gateway URL.
                </p>
              </div>
              <div className="space-y-2 rounded-lg border border-border p-4 text-sm">
                <p className="flex items-center gap-2">
                  <Server className="h-4 w-4 text-muted-foreground" />
                  <span>
                    <strong>{server?.name}</strong>
                    {server ? ` · ${tools.length || server.tool_count} tools` : ''}
                  </span>
                </p>
                <p>
                  Key: <strong>{keyRes?.name}</strong>
                </p>
                <p className="break-all text-muted-foreground">
                  Your teammate can connect at:{' '}
                  <code className="text-foreground">{endpointURL}</code> with their own key
                </p>
              </div>
              <Button
                className="w-full"
                onClick={() => void finish()}
                disabled={finishing}
                aria-label={t('wizard.goDashboard')}
              >
                {finishing ? t('common.loading') : t('wizard.goDashboard')}
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
