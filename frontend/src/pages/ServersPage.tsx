import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  api,
  type CircuitStatus,
  type CreateServerInput,
  type MCPServer,
  type OAuthConnectSession,
} from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableLoading,
  TableRow,
} from '@/components/ui/table'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { ServerDetailDrawer } from '@/components/ServerDetailDrawer'
import { GuidedServerEmpty } from '@/components/GuidedServerEmpty'
import { ServerQuickAddGrid } from '@/components/ServerQuickAddGrid'
import { useToast } from '@/components/Toast'
import { cn } from '@/lib/utils'
import { createInputFromPreset } from '@/lib/createInputFromPreset'
import { hintForError, hostFromUrl } from '@/lib/hintForError'
import type { ServerPreset } from '@/data/catalog'
import { CheckCircle2, Copy, ExternalLink, Link2, Power, RefreshCw, Trash2 } from 'lucide-react'

type AuthMode = 'none' | 'static' | 'oauth'
type TransportMode = 'http' | 'stdio'
type DialogTab = 'quick' | 'advanced'

function statusBadge(server: MCPServer, circuit?: CircuitStatus) {
  if (!server.enabled) return <Badge variant="secondary">disabled</Badge>
  if (server.transport === 'stdio') {
    const st = circuit?.process?.state || server.process?.state
    const chip = <Badge variant="secondary">stdio</Badge>
    if (st === 'error' || server.last_index_error) {
      return (
        <span className="inline-flex items-center gap-1.5">
          {chip}
          <Badge variant="destructive">error</Badge>
        </span>
      )
    }
    if (st === 'starting') {
      return (
        <span className="inline-flex items-center gap-1.5">
          {chip}
          <Badge variant="warning">starting</Badge>
        </span>
      )
    }
    return (
      <span className="inline-flex items-center gap-1.5">
        {chip}
        <Badge variant="success">running</Badge>
      </span>
    )
  }
  if (server.oauth_status === 'expired') {
    return <Badge variant="warning">reconnect needed</Badge>
  }
  if (server.oauth_status === 'pending') {
    return <Badge variant="secondary">waiting for sign-in…</Badge>
  }
  if (server.oauth_status === 'connected') {
    return <Badge variant="success">connected</Badge>
  }
  if (
    (server.auth_type === 'oauth_device' || server.auth_type === 'oauth_pkce') &&
    (!server.oauth_status || server.oauth_status === 'disconnected')
  ) {
    return <Badge variant="secondary">not connected</Badge>
  }
  if (server.last_index_error) {
    return (
      <span title={server.last_index_error}>
        <Badge variant="destructive">index failed</Badge>
      </span>
    )
  }
  switch (circuit?.state) {
    case 'open':
      return <Badge variant="destructive">disconnected</Badge>
    case 'half_open':
      return <Badge variant="warning">degraded</Badge>
    default:
      return (
        <span className="inline-flex items-center gap-1.5">
          <Badge variant="secondary">http</Badge>
          <Badge variant="success">healthy</Badge>
        </span>
      )
  }
}

const highlightClass = 'ring-2 ring-primary/50 transition-shadow duration-150'

export function ServersPage() {
  const { t } = useTranslation()
  const { push } = useToast()
  const [servers, setServers] = useState<MCPServer[]>([])
  const [statusMap, setStatusMap] = useState<Record<string, CircuitStatus>>({})
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [dialogTab, setDialogTab] = useState<DialogTab>('quick')
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [transport, setTransport] = useState<TransportMode>('http')
  const [command, setCommand] = useState('')
  const [argsText, setArgsText] = useState('')
  const [envText, setEnvText] = useState('')
  const [workdir, setWorkdir] = useState('')
  const [pasteRaw, setPasteRaw] = useState('')
  const [cmdWarn, setCmdWarn] = useState('')
  const [authMode, setAuthMode] = useState<AuthMode>('none')
  const [authHeader, setAuthHeader] = useState('')
  const [authValue, setAuthValue] = useState('')
  const [authValueHint, setAuthValueHint] = useState('')
  const [keyLabel, setKeyLabel] = useState('')
  const [keyUrl, setKeyUrl] = useState('')
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [addingId, setAddingId] = useState<string | null>(null)
  const [highlight, setHighlight] = useState(false)
  const [retesting, setRetesting] = useState<string | null>(null)
  const [detailServer, setDetailServer] = useState<MCPServer | null>(null)
  const [toolPolicy, setToolPolicy] = useState(false)
  const [isAdmin, setIsAdmin] = useState(true)
  const authValueRef = useRef<HTMLInputElement>(null)

  const [connectServerId, setConnectServerId] = useState<string | null>(null)
  const [connectPhase, setConnectPhase] = useState<'idle' | 'pending' | 'connected' | 'expired'>('idle')
  const [connectSession, setConnectSession] = useState<OAuthConnectSession | null>(null)
  const [connectError, setConnectError] = useState('')

  const load = useCallback(async () => {
    setError('')
    try {
      const [list, me] = await Promise.all([api.listServers(), api.me().catch(() => null)])
      if (me) {
        setToolPolicy(!!me.tool_policy)
        setIsAdmin(me.role === 'admin')
      }
      setServers(list)
      const statuses = await Promise.all(
        list.map(async (s) => {
          try {
            return [s.id, await api.serverStatus(s.id)] as const
          } catch {
            return [s.id, { state: 'closed' }] as const
          }
        }),
      )
      setStatusMap(Object.fromEntries(statuses))
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : 'Failed to load servers')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!connectServerId || connectPhase !== 'pending') return
    const intervalMs = Math.max(3000, (connectSession?.interval ?? 3) * 1000)
    const t = setInterval(async () => {
      try {
        const st = await api.connectStatus(connectServerId)
        if (st.status === 'connected') {
          setConnectPhase('connected')
          push({ title: 'Server connected', tone: 'success' })
          await load()
        } else if (st.status === 'disconnected' || (st.expires_in !== undefined && st.expires_in <= 0)) {
          setConnectPhase('expired')
          setConnectError('code expired — retry')
        } else if (st.verification_uri) {
          setConnectSession((prev) =>
            prev
              ? {
                  ...prev,
                  verification_uri: st.verification_uri,
                  verification_code: st.verification_code,
                  expires_in: st.expires_in ?? prev.expires_in,
                }
              : prev,
          )
        }
      } catch (err) {
        console.error(err)
      }
    }, intervalMs)
    return () => clearInterval(t)
  }, [connectServerId, connectPhase, connectSession?.interval, load, push])

  const startConnect = async (serverId: string, reconnect = false) => {
    setConnectServerId(serverId)
    setConnectPhase('pending')
    setConnectError('')
    setConnectSession(null)
    try {
      const sess = reconnect
        ? await api.reconnectServer(serverId, 'device')
        : await api.connectServer(serverId, 'device')
      setConnectSession(sess)
      if (sess.authorize_url) {
        window.open(sess.authorize_url, '_blank', 'noopener,noreferrer')
      }
    } catch (err) {
      console.error(err)
      setConnectError(err instanceof Error ? err.message : 'Connect failed')
      setConnectPhase('expired')
    }
  }

  const toggleEnabled = async (server: MCPServer, enabled: boolean) => {
    setServers((prev) => prev.map((s) => (s.id === server.id ? { ...s, enabled } : s)))
    try {
      const updated = await api.patchServer(server.id, { enabled })
      setServers((prev) => prev.map((s) => (s.id === server.id ? { ...s, ...updated } : s)))
      push({
        title: enabled ? 'Server enabled' : 'Server disabled',
        tone: 'success',
      })
    } catch (err) {
      console.error(err)
      setServers((prev) =>
        prev.map((s) => (s.id === server.id ? { ...s, enabled: server.enabled } : s)),
      )
      setError(err instanceof Error ? err.message : 'Failed to update server')
    }
  }

  const remove = async (id: string) => {
    try {
      await api.deleteServer(id)
      setServers((prev) => prev.filter((s) => s.id !== id))
      setDeleteId(null)
      push({ title: 'Server deleted', tone: 'success' })
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : 'Failed to delete server')
    }
  }

  const retest = async (id: string) => {
    setRetesting(id)
    try {
      const res = await api.reindexServer(id)
      setServers((prev) =>
        prev.map((s) =>
          s.id === id
            ? {
                ...s,
                tool_count: res.tool_count,
                last_index_error: res.indexing_error || '',
              }
            : s,
        ),
      )
      const st = await api.serverStatus(id)
      setStatusMap((prev) => ({ ...prev, [id]: st }))
      push({ title: 'Reindex complete', tone: 'success' })
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : 'Reindex failed')
    } finally {
      setRetesting(null)
    }
  }

  const resetForm = () => {
    setName('')
    setBaseUrl('')
    setTransport('http')
    setCommand('')
    setArgsText('')
    setEnvText('')
    setWorkdir('')
    setPasteRaw('')
    setCmdWarn('')
    setAuthMode('none')
    setAuthHeader('')
    setAuthValue('')
    setAuthValueHint('')
    setKeyLabel('')
    setKeyUrl('')
    setFieldErrors({})
    setHighlight(false)
  }

  const checkCommandNow = async (cmd: string) => {
    if (!cmd.trim()) {
      setCmdWarn('')
      return
    }
    try {
      const res = await api.checkCommand(cmd.trim())
      setCmdWarn(res.found ? '' : res.error || 'Command not found in PATH')
    } catch {
      setCmdWarn('')
    }
  }

  const applyPresetToForm = (preset: ServerPreset, opts?: { focusKey?: boolean }) => {
    setName(preset.name)
    setFieldErrors({})
    setAuthValueHint(preset.auth.value_hint || '')
    setKeyLabel(preset.auth.key_label || '')
    setKeyUrl(preset.auth.key_url || '')

    if (preset.transport === 'stdio') {
      setTransport('stdio')
      setCommand(preset.command || '')
      setArgsText((preset.args || []).join('\n'))
      setEnvText(
        (preset.env || []).map((e) => `${e.key}=`).join('\n') ||
          '',
      )
      setBaseUrl('')
      setAuthMode('none')
      setAuthHeader('')
      setAuthValue('')
      void checkCommandNow(preset.command || '')
    } else {
      setTransport('http')
      setBaseUrl(preset.base_url || '')
      setCommand('')
      setArgsText('')
      setEnvText('')
      setCmdWarn('')
      if (preset.auth.type === 'none') {
        setAuthMode('none')
        setAuthHeader('')
        setAuthValue('')
      } else if (preset.auth.type === 'oauth_device') {
        setAuthMode('oauth')
        setAuthHeader('')
        setAuthValue('')
      } else {
        setAuthMode('static')
        setAuthHeader(preset.auth.header || 'Authorization')
        setAuthValue('')
      }
    }

    setDialogTab('advanced')
    setModalOpen(true)
    setHighlight(true)
    window.setTimeout(() => setHighlight(false), 600)

    if (opts?.focusKey || preset.auth.type === 'static') {
      // Focus after tab switch / dialog paint (next frame).
      requestAnimationFrame(() => authValueRef.current?.focus())
    }
  }

  const showCreateError = (msg: string, input: CreateServerInput, preset?: ServerPreset) => {
    const hint = hintForError(msg, {
      keyLabel: preset?.auth.key_label || keyLabel || undefined,
      command: input.command,
      host: hostFromUrl(input.base_url || ''),
    })
    setFieldErrors({
      form: hint ? `${msg}\n${hint}` : msg,
    })
    setDialogTab('advanced')
    setModalOpen(true)
  }

  const submitCreate = async (
    input: CreateServerInput,
    opts?: { preset?: ServerPreset; oauth?: boolean },
  ) => {
    const created = await api.createServer(input)
    if (created.indexing_error) {
      showCreateError(created.indexing_error, input, opts?.preset)
      // Server was created — refresh list but keep dialog open with hint.
      await load()
      return null
    }
    return created
  }

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    const errs: Record<string, string> = {}
    if (!name.trim()) errs.name = 'Name is required'
    if (transport === 'http') {
      if (!baseUrl.trim()) errs.url = 'URL is required'
      if (authMode === 'static' && !authValue.trim()) errs.auth = 'API key value is required'
    } else {
      if (!command.trim()) errs.command = 'Command is required'
    }
    setFieldErrors(errs)
    if (Object.keys(errs).length) return

    setSaving(true)
    setError('')
    try {
      let input: CreateServerInput
      if (transport === 'stdio') {
        const args = argsText
          .split('\n')
          .map((l) => l.trim())
          .filter(Boolean)
        const env: Record<string, string> = {}
        for (const line of envText.split('\n')) {
          const trimmed = line.trim()
          if (!trimmed || trimmed.startsWith('#')) continue
          const eq = trimmed.indexOf('=')
          if (eq <= 0) continue
          const v = trimmed.slice(eq + 1)
          if (v !== '') env[trimmed.slice(0, eq)] = v
        }
        input = {
          name,
          transport: 'stdio',
          command: command.trim(),
          args,
          env,
          workdir: workdir.trim() || undefined,
          auth: { type: 'none' },
        }
      } else {
        const auth =
          authMode === 'none'
            ? { type: 'none' as const }
            : authMode === 'static'
              ? {
                  type: 'static' as const,
                  header: authHeader || 'Authorization',
                  value: authValue,
                }
              : { type: 'oauth_device' as const }
        input = {
          name,
          base_url: baseUrl,
          auth,
          transport: 'http',
        }
      }

      const created = await submitCreate(input, { oauth: authMode === 'oauth' })
      if (!created) {
        setSaving(false)
        return
      }
      setModalOpen(false)
      resetForm()
      push({ title: 'Server added', tone: 'success' })
      await load()
      if (authMode === 'oauth') {
        await startConnect(created.id)
      }
    } catch (err) {
      console.error(err)
      const msg = err instanceof Error ? err.message : 'Failed to create server'
      showCreateError(msg, {
        name,
        base_url: baseUrl,
        command,
        transport,
      })
    } finally {
      setSaving(false)
    }
  }

  const addNow = async (preset: ServerPreset) => {
    setAddingId(preset.id)
    setFieldErrors({})
    try {
      const input = createInputFromPreset(preset)
      const created = await submitCreate(input, { preset })
      if (!created) return
      setModalOpen(false)
      resetForm()
      push({ title: `${preset.name} added`, tone: 'success' })
      await load()
      if (preset.auth.type === 'oauth_device') {
        await startConnect(created.id)
      }
    } catch (err) {
      console.error(err)
      applyPresetToForm(preset)
      const msg = err instanceof Error ? err.message : 'Failed to create server'
      showCreateError(msg, createInputFromPreset(preset), preset)
    } finally {
      setAddingId(null)
    }
  }

  const onSelectPreset = (preset: ServerPreset) => {
    applyPresetToForm(preset)
  }

  const applyPaste = async () => {
    try {
      const res = await api.parseMCPJSON(pasteRaw)
      const first = res.servers[0]
      if (!first) {
        setFieldErrors({ paste: 'No servers found in paste' })
        return
      }
      if (first.name) setName(first.name)
      if (first.transport === 'stdio') {
        setTransport('stdio')
        setCommand(first.command || '')
        setArgsText((first.args || []).join('\n'))
        setEnvText(
          Object.entries(first.env || {})
            .map(([k, v]) => `${k}=${v}`)
            .join('\n'),
        )
        void checkCommandNow(first.command || '')
      } else {
        setTransport('http')
        setBaseUrl(first.base_url || '')
      }
      setFieldErrors({})
      push({ title: 'Pasted config applied', tone: 'success' })
    } catch (err) {
      setFieldErrors({ paste: err instanceof Error ? err.message : 'Invalid mcp.json' })
    }
  }

  const onCommandBlur = async () => {
    if (transport !== 'stdio' || !command.trim()) {
      setCmdWarn('')
      return
    }
    await checkCommandNow(command)
  }

  const openAddDialog = (tab: DialogTab = 'quick') => {
    resetForm()
    setDialogTab(tab)
    setModalOpen(true)
  }

  const deleteTarget = servers.find((s) => s.id === deleteId)
  const copyUri = async () => {
    if (!connectSession?.verification_uri) return
    try {
      await navigator.clipboard.writeText(connectSession.verification_uri)
      push({ title: 'Link copied', tone: 'success' })
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('servers.title')}
        description={t('servers.description')}
        actions={
          isAdmin ? (
            <Button onClick={() => openAddDialog('quick')} aria-label={t('servers.addServer')}>
              {t('servers.addServer')}
            </Button>
          ) : undefined
        }
      />

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}

      <div className="rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>{t('servers.name')}</TableHead>
              <TableHead>{t('servers.status')}</TableHead>
              <TableHead className="text-right">{t('servers.tools')}</TableHead>
              <TableHead className="hidden md:table-cell">{t('servers.url')}</TableHead>
              <TableHead className="w-36 text-right">{t('servers.actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && <TableLoading colSpan={5} />}
            {!loading && servers.length === 0 && (
              <tr>
                <td colSpan={5} className="p-0">
                  <GuidedServerEmpty
                    onSelect={(p) => {
                      applyPresetToForm(p)
                    }}
                    onAddNow={(p) => void addNow(p)}
                    onBrowseAll={() => openAddDialog('quick')}
                    addingId={addingId}
                  />
                </td>
              </tr>
            )}
            {!loading &&
              servers.map((s) => (
                <TableRow
                  key={s.id}
                  className={cn(!s.enabled && 'opacity-60', 'cursor-pointer')}
                  onClick={() => setDetailServer(s)}
                >
                  <TableCell>
                    <div className="flex items-center gap-2 font-medium">
                      {s.name}
                    </div>
                  </TableCell>
                  <TableCell>{statusBadge(s, statusMap[s.id])}</TableCell>
                  <TableCell className="text-right tabular-nums">{s.tool_count}</TableCell>
                  <TableCell className="hidden max-w-[220px] truncate text-muted-foreground md:table-cell">
                    {s.transport === 'stdio' ? s.command || 'stdio' : s.base_url}
                  </TableCell>
                  <TableCell className="text-right">
                    <div
                      className="flex justify-end gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {(s.auth_type === 'oauth_device' || s.auth_type === 'oauth_pkce') &&
                        (s.oauth_status === 'disconnected' ||
                          s.oauth_status === 'expired' ||
                          !s.oauth_status) && (
                          <Button
                            variant="ghost"
                            size="icon"
                            title={t('servers.connect')}
                            aria-label={`Connect ${s.name}`}
                            onClick={() =>
                              void startConnect(s.id, s.oauth_status === 'expired')
                            }
                          >
                            <Icon icon={Link2} className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      <Button
                        variant="ghost"
                        size="icon"
                        title={s.enabled ? 'Disable' : 'Enable'}
                        aria-label={s.enabled ? `Disable ${s.name}` : `Enable ${s.name}`}
                        onClick={() => void toggleEnabled(s, !s.enabled)}
                      >
                        <Icon icon={Power} className="h-3.5 w-3.5" />
                      </Button>
                      {(statusMap[s.id]?.state === 'open' || s.last_index_error) && (
                        <Button
                          variant="ghost"
                          size="icon"
                          title={t('servers.reindex')}
                          aria-label={`Reindex ${s.id}`}
                          disabled={retesting === s.id}
                          onClick={() => void retest(s.id)}
                        >
                          <Icon
                            icon={RefreshCw}
                            className={cn('h-3.5 w-3.5', retesting === s.id && 'animate-spin')}
                          />
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="icon"
                        title={t('servers.delete')}
                        aria-label={`Delete ${s.name}`}
                        onClick={() => setDeleteId(s.id)}
                      >
                        <Icon icon={Trash2} className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>

      {detailServer && (
        <ServerDetailDrawer
          server={detailServer}
          toolPolicy={toolPolicy}
          onClose={() => setDetailServer(null)}
          onRetest={(id) => {
            void retest(id).then(() => {
              void api.listServerTools(id).then(() => load())
            })
          }}
        />
      )}

      <Dialog
        open={modalOpen}
        onClose={() => {
          setModalOpen(false)
          resetForm()
        }}
        title={t('servers.addTitle')}
        description="Pick a preset or configure manually"
        className="!max-w-2xl"
      >
        <div
          className="mb-4 grid grid-cols-2 gap-1 rounded-md border border-border p-1"
          role="tablist"
          aria-label="Add mode"
        >
          {(
            [
              ['quick', 'Quick add'],
              ['advanced', 'Advanced'],
            ] as const
          ).map(([tab, label]) => (
            <button
              key={tab}
              type="button"
              role="tab"
              aria-selected={dialogTab === tab}
              className={cn(
                'rounded px-2 py-1.5 text-xs font-medium transition-colors',
                dialogTab === tab
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )}
              onClick={() => setDialogTab(tab)}
            >
              {label}
            </button>
          ))}
        </div>

        {dialogTab === 'quick' ? (
          <ServerQuickAddGrid
            onSelect={onSelectPreset}
            onAddNow={(p) => void addNow(p)}
            addingId={addingId}
          />
        ) : (
          <form onSubmit={onCreate} className="space-y-4" data-testid="advanced-server-form">
            <div className="space-y-1.5">
              <Label htmlFor="srv-paste">Paste your mcp.json</Label>
              <textarea
                id="srv-paste"
                className="min-h-[72px] w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs"
                placeholder='{"mcpServers":{"fs":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/tmp"]}}}'
                value={pasteRaw}
                onChange={(e) => setPasteRaw(e.target.value)}
              />
              <Button type="button" variant="outline" size="sm" onClick={() => void applyPaste()}>
                Apply paste
              </Button>
              {fieldErrors.paste && (
                <p className="text-xs text-destructive">{fieldErrors.paste}</p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="srv-name">Name</Label>
              <Input
                id="srv-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className={cn(highlight && highlightClass)}
              />
              {fieldErrors.name && (
                <p className="text-xs text-destructive">{fieldErrors.name}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label>Transport</Label>
              <div
                className="grid grid-cols-2 gap-1 rounded-md border border-border p-1"
                role="tablist"
                aria-label="Transport"
              >
                {(
                  [
                    ['http', 'Remote (HTTP)'],
                    ['stdio', 'Local (stdio)'],
                  ] as const
                ).map(([mode, label]) => (
                  <button
                    key={mode}
                    type="button"
                    role="tab"
                    aria-selected={transport === mode}
                    className={cn(
                      'rounded px-2 py-1.5 text-xs font-medium transition-colors',
                      transport === mode
                        ? 'bg-accent text-accent-foreground'
                        : 'text-muted-foreground hover:text-foreground',
                    )}
                    onClick={() => setTransport(mode)}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>

            {transport === 'http' ? (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="srv-url">Base URL</Label>
                  <Input
                    id="srv-url"
                    type="url"
                    placeholder="https://mcp.deepwiki.com/mcp"
                    className={cn('font-mono text-xs', highlight && highlightClass)}
                    value={baseUrl}
                    onChange={(e) => setBaseUrl(e.target.value)}
                  />
                  {fieldErrors.url && (
                    <p className="text-xs text-destructive">{fieldErrors.url}</p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label>Auth</Label>
                  <div
                    className="grid grid-cols-3 gap-1 rounded-md border border-border p-1"
                    role="tablist"
                    aria-label="Auth type"
                  >
                    {(
                      [
                        ['none', 'None'],
                        ['static', 'API key'],
                        ['oauth', 'OAuth sign-in'],
                      ] as const
                    ).map(([mode, label]) => (
                      <button
                        key={mode}
                        type="button"
                        role="tab"
                        aria-selected={authMode === mode}
                        className={cn(
                          'rounded px-2 py-1.5 text-xs font-medium transition-colors',
                          authMode === mode
                            ? 'bg-accent text-accent-foreground'
                            : 'text-muted-foreground hover:text-foreground',
                        )}
                        onClick={() => setAuthMode(mode)}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                  {authMode === 'static' && (
                    <div className="grid gap-4 pt-1 sm:grid-cols-2">
                      <div className="space-y-1.5">
                        <Label htmlFor="srv-auth-h">Auth header</Label>
                        <Input
                          id="srv-auth-h"
                          placeholder="Authorization"
                          value={authHeader}
                          onChange={(e) => setAuthHeader(e.target.value)}
                          className={cn(highlight && highlightClass)}
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="srv-auth-v">
                          {keyLabel || 'Auth value'}
                        </Label>
                        <Input
                          id="srv-auth-v"
                          ref={authValueRef}
                          type="password"
                          placeholder={authValueHint || 'Bearer …'}
                          value={authValue}
                          onChange={(e) => setAuthValue(e.target.value)}
                          className={cn(highlight && highlightClass)}
                        />
                        {authValueHint && (
                          <p className="text-[11px] text-muted-foreground">
                            Format: {authValueHint}
                            {keyUrl && (
                              <>
                                {' · '}
                                <a
                                  href={keyUrl}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="underline underline-offset-2 hover:text-foreground"
                                >
                                  Get key
                                </a>
                              </>
                            )}
                          </p>
                        )}
                      </div>
                    </div>
                  )}
                  {authMode === 'oauth' && (
                    <p className="text-xs text-muted-foreground">
                      After adding, you&apos;ll sign in via device code (or browser redirect) to
                      connect this server.
                    </p>
                  )}
                  {fieldErrors.auth && (
                    <p className="text-xs text-destructive">{fieldErrors.auth}</p>
                  )}
                </div>
              </>
            ) : (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="srv-cmd">Command</Label>
                  <Input
                    id="srv-cmd"
                    className={cn('font-mono text-xs', highlight && highlightClass)}
                    placeholder="npx"
                    value={command}
                    onChange={(e) => setCommand(e.target.value)}
                    onBlur={() => void onCommandBlur()}
                  />
                  {cmdWarn && (
                    <p className="text-xs text-amber-600 dark:text-amber-400">{cmdWarn}</p>
                  )}
                  {fieldErrors.command && (
                    <p className="text-xs text-destructive">{fieldErrors.command}</p>
                  )}
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="srv-args">Args (one per line)</Label>
                  <textarea
                    id="srv-args"
                    className={cn(
                      'min-h-[72px] w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs',
                      highlight && highlightClass,
                    )}
                    placeholder={'-y\n@modelcontextprotocol/server-filesystem\n/tmp'}
                    value={argsText}
                    onChange={(e) => setArgsText(e.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="srv-env">Env (KEY=VALUE, one per line)</Label>
                  <textarea
                    id="srv-env"
                    className={cn(
                      'min-h-[56px] w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs',
                      highlight && highlightClass,
                    )}
                    placeholder="GITHUB_PERSONAL_ACCESS_TOKEN=ghp_…"
                    value={envText}
                    onChange={(e) => setEnvText(e.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="srv-workdir">Workdir (optional)</Label>
                  <Input
                    id="srv-workdir"
                    className="font-mono text-xs"
                    placeholder="Defaults to ~/.tokencontrolplane/sandbox/<id>"
                    value={workdir}
                    onChange={(e) => setWorkdir(e.target.value)}
                  />
                </div>
              </>
            )}

            {fieldErrors.form && (
              <p className="whitespace-pre-line text-xs text-destructive">{fieldErrors.form}</p>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setModalOpen(false)
                  resetForm()
                }}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving
                  ? 'Adding…'
                  : transport === 'http' && authMode === 'oauth'
                    ? 'Add & connect'
                    : t('servers.addServer')}
              </Button>
            </div>
          </form>
        )}
      </Dialog>

      <Dialog
        open={!!connectServerId}
        onClose={() => {
          setConnectServerId(null)
          setConnectPhase('idle')
          setConnectSession(null)
        }}
        title={
          connectPhase === 'connected'
            ? 'Connected'
            : connectPhase === 'expired'
              ? 'Code expired'
              : 'Sign in to connect'
        }
        description={
          connectPhase === 'connected'
            ? 'Upstream OAuth token stored. Tools will index shortly.'
            : 'Open the link, sign in, then wait — this dialog updates automatically.'
        }
      >
        <div className="space-y-4">
          {connectPhase === 'pending' && connectSession && (
            <>
              {connectSession.verification_code && (
                <p className="text-center font-mono text-3xl font-semibold tracking-widest">
                  {connectSession.verification_code}
                </p>
              )}
              {connectSession.verification_uri && (
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    readOnly
                    className="font-mono text-xs"
                    value={connectSession.verification_uri}
                  />
                  <div className="flex gap-1">
                    <Button type="button" variant="outline" size="icon" onClick={() => void copyUri()}>
                      <Icon icon={Copy} className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() =>
                        window.open(
                          connectSession.verification_uri,
                          '_blank',
                          'noopener,noreferrer',
                        )
                      }
                    >
                      <Icon icon={ExternalLink} className="mr-1.5 h-3.5 w-3.5" />
                      Open in browser
                    </Button>
                  </div>
                </div>
              )}
              <p className="text-center text-xs text-muted-foreground">
                Waiting for sign-in…
                {connectSession.expires_in
                  ? ` (expires in ~${connectSession.expires_in}s)`
                  : ''}
              </p>
            </>
          )}
          {connectPhase === 'connected' && (
            <div className="flex flex-col items-center gap-2 py-4 text-emerald-600">
              <Icon icon={CheckCircle2} className="h-10 w-10" />
              <p className="text-sm font-medium text-foreground">You&apos;re connected</p>
            </div>
          )}
          {(connectPhase === 'expired' || connectError) && (
            <p className="text-sm text-destructive">{connectError || 'code expired — retry'}</p>
          )}
          <div className="flex justify-end gap-2">
            {connectPhase === 'expired' && connectServerId && (
              <Button onClick={() => void startConnect(connectServerId, true)}>Retry</Button>
            )}
            <Button
              variant="outline"
              onClick={() => {
                setConnectServerId(null)
                setConnectPhase('idle')
              }}
            >
              {connectPhase === 'connected' ? 'Done' : 'Close'}
            </Button>
          </div>
        </div>
      </Dialog>

      <Dialog
        open={!!deleteId}
        onClose={() => setDeleteId(null)}
        title={`Delete server${deleteTarget ? ` '${deleteTarget.name}'` : ''}?`}
        description="Indexed tools for this server will be removed. This cannot be undone."
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setDeleteId(null)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={() => deleteId && void remove(deleteId)}
          >
            Delete server
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
