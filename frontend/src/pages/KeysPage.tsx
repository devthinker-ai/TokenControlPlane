import { FormEvent, useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type APIKey, type CreateKeyResponse } from '@/lib/api'
import { formatRelative, formatTokens, cn } from '@/lib/utils'
import { fmtDate } from '@/lib/fmt'
import { KeyReveal } from '@/components/KeyReveal'
import { KeyDetailDrawer } from '@/components/KeyDetailDrawer'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { useToast } from '@/components/Toast'
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
  TableEmpty,
  TableHead,
  TableHeader,
  TableLoading,
  TableRow,
} from '@/components/ui/table'
import { KeyRound, Power, Trash2 } from 'lucide-react'

export function KeysPage() {
  const { t } = useTranslation()
  const { push } = useToast()
  const [keys, setKeys] = useState<APIKey[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [killTarget, setKillTarget] = useState<APIKey | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [saving, setSaving] = useState(false)
  const [created, setCreated] = useState<CreateKeyResponse | null>(null)
  const [now, setNow] = useState(Date.now())
  const [toolPolicy, setToolPolicy] = useState(false)
  const [detailKey, setDetailKey] = useState<APIKey | null>(null)
  const [meId, setMeId] = useState('')
  const [isAdmin, setIsAdmin] = useState(true)

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 5 * 60_000)
    return () => window.clearInterval(id)
  }, [])

  const load = useCallback(async () => {
    setError('')
    try {
      const [list, me] = await Promise.all([api.listKeys(), api.me().catch(() => null)])
      setKeys(list)
      if (me) {
        setToolPolicy(!!me.tool_policy)
        setMeId(me.id)
        setIsAdmin(me.role === 'admin')
      }
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('keys.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const closeModal = () => {
    setModalOpen(false)
    setCreated(null)
    setName('')
  }

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      const res = await api.createKey(name)
      setCreated(res)
      await load()
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('keys.createFailed'))
    } finally {
      setSaving(false)
    }
  }

  const confirmKill = async () => {
    if (!killTarget) return
    try {
      await api.killKey(killTarget.id)
      setKeys((prev) =>
        prev.map((k) => (k.id === killTarget.id ? { ...k, killed: true } : k)),
      )
      setKillTarget(null)
      push({
        title: t('keys.toastKilled'),
        tone: 'warning',
      })
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('keys.killFailed'))
    }
  }

  const unkill = async (key: APIKey) => {
    try {
      await api.unkillKey(key.id)
      setKeys((prev) => prev.map((k) => (k.id === key.id ? { ...k, killed: false } : k)))
      push({ title: t('keys.toastEnabled'), tone: 'success' })
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('keys.unkillFailed'))
    }
  }

  const remove = async (id: string) => {
    try {
      await api.deleteKey(id)
      setKeys((prev) => prev.filter((k) => k.id !== id))
      setDeleteId(null)
      push({ title: t('keys.toastDeleted'), tone: 'success' })
    } catch (err) {
      console.error(err)
      setError(err instanceof Error ? err.message : t('keys.deleteFailed'))
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('keys.title')}
        description={t('keys.description')}
        actions={
          <Button onClick={() => setModalOpen(true)} aria-label={t('keys.generate')}>
            {t('keys.generate')}
          </Button>
        }
      />

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}

      <div className="rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead>{t('keys.name')}</TableHead>
              <TableHead className="hidden sm:table-cell">{t('keys.created')}</TableHead>
              <TableHead className="hidden md:table-cell">{t('keys.lastUsed')}</TableHead>
              <TableHead>{t('keys.status')}</TableHead>
              <TableHead className="hidden lg:table-cell">{t('keys.access')}</TableHead>
              <TableHead className="hidden lg:table-cell text-right">{t('keys.usage')}</TableHead>
              <TableHead className="w-28 text-right">{t('common.actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && <TableLoading colSpan={7} />}
            {!loading && keys.length === 0 && (
              <TableEmpty
                colSpan={7}
                icon={KeyRound}
                title={t('keys.emptyTitle')}
                description={t('keys.emptyDesc')}
                actionLabel={t('keys.generate')}
                onAction={() => setModalOpen(true)}
              />
            )}
            {!loading &&
              keys.map((k) => {
                const pct =
                  k.monthly_budget > 0
                    ? Math.min(100, (k.tokens_used / k.monthly_budget) * 100)
                    : 0
                const canEdit = isAdmin || k.owner?.id === meId
                return (
                  <TableRow
                    key={k.id}
                    className={cn(k.killed && 'opacity-60', 'cursor-pointer')}
                    onClick={() => setDetailKey(k)}
                  >
                    <TableCell>
                      <div className="font-medium">{k.name}</div>
                      {k.owner?.name ? (
                        <span className="text-xs text-muted-foreground">by {k.owner.name}</span>
                      ) : (
                        <span className="text-xs text-muted-foreground">{t('keys.shared')}</span>
                      )}
                      {k.prefix && (
                        <code className="block text-xs tabular-nums text-muted-foreground">
                          {k.prefix}…
                        </code>
                      )}
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground sm:table-cell">
                      <span
                        className="tabular-nums"
                        title={k.created_at || undefined}
                      >
                        {k.created_at ? fmtDate(k.created_at) : '—'}
                      </span>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground md:table-cell">
                      <span className="tabular-nums" title={k.last_used_at || undefined}>
                        {formatRelative(k.last_used_at, now)}
                      </span>
                    </TableCell>
                    <TableCell>
                      {k.killed ? (
                        <Badge variant="destructive">{t('keys.killed')}</Badge>
                      ) : (
                        <Badge variant="success">{t('keys.active')}</Badge>
                      )}
                    </TableCell>
                    <TableCell className="hidden lg:table-cell">
                      <div className="flex flex-wrap gap-1">
                        <Badge variant="secondary">
                          {k.server_policy?.mode === 'custom'
                            ? `${k.server_policy.count ?? 0} servers`
                            : 'All servers'}
                        </Badge>
                        <Badge variant="secondary">
                          {k.tool_policy?.mode === 'custom'
                            ? `${k.tool_policy.count ?? k.tool_policy.allowed?.length ?? 0} tools`
                            : 'All tools'}
                        </Badge>
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-right lg:table-cell">
                      <span className="tabular-nums text-xs text-muted-foreground">
                        {formatTokens(k.tokens_used)}
                        {k.monthly_budget > 0 ? ` / ${formatTokens(k.monthly_budget)}` : ''}
                        {k.monthly_budget > 0 ? ` (${pct.toFixed(0)}%)` : ''}
                      </span>
                    </TableCell>
                    <TableCell className="text-right">
                      <div
                        className="flex justify-end gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {k.killed ? (
                          <Button
                            variant="ghost"
                            size="icon"
                            title={canEdit ? t('keys.unkill') : 'admin only / not your key'}
                            aria-label={`Re-enable ${k.name}`}
                            disabled={!canEdit}
                            onClick={() => void unkill(k)}
                          >
                            <Icon icon={Power} className="h-3.5 w-3.5" />
                          </Button>
                        ) : (
                          <Button
                            variant="ghost"
                            size="icon"
                            title={t('keys.kill')}
                            aria-label={`Kill ${k.name}`}
                            onClick={() => setKillTarget(k)}
                          >
                            <Icon icon={Power} className="h-3.5 w-3.5 text-destructive" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          title={canEdit ? t('common.delete') : 'admin only / not your key'}
                          aria-label={`Delete ${k.name}`}
                          disabled={!canEdit}
                          onClick={() => setDeleteId(k.id)}
                        >
                          <Icon icon={Trash2} className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
          </TableBody>
        </Table>
      </div>

      <Dialog
        open={modalOpen}
        onClose={closeModal}
        title={created ? t('keys.revealOnce') : t('keys.generate')}
        description={created ? undefined : t('keys.description')}
        className={created ? 'max-w-xl' : undefined}
      >
        {created ? (
          <div className="space-y-4">
            <KeyReveal plaintextKey={created.key} snippets={created.snippets} />
            <div className="flex justify-end">
              <Button onClick={closeModal}>{t('keys.done')}</Button>
            </div>
          </div>
        ) : (
          <form onSubmit={onCreate} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="key-name">{t('keys.name')}</Label>
              <Input
                id="key-name"
                placeholder={t('keys.namePlaceholder')}
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={closeModal}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={saving}>
                {saving ? t('common.loading') : t('keys.generate')}
              </Button>
            </div>
          </form>
        )}
      </Dialog>

      <Dialog
        open={!!killTarget}
        onClose={() => setKillTarget(null)}
        title={t('keys.killTitle')}
        description={t('keys.killBody')}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setKillTarget(null)}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={() => void confirmKill()}>
            {t('keys.kill')}
          </Button>
        </div>
      </Dialog>

      <Dialog
        open={!!deleteId}
        onClose={() => setDeleteId(null)}
        title={t('keys.deleteTitle')}
        description={t('keys.deleteBody')}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setDeleteId(null)}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="destructive"
            onClick={() => deleteId && void remove(deleteId)}
          >
            {t('keys.delete')}
          </Button>
        </div>
      </Dialog>

      {detailKey && (
        <KeyDetailDrawer
          keyRow={detailKey}
          toolPolicy={toolPolicy}
          onClose={() => setDetailKey(null)}
          onSaved={() => void load()}
        />
      )}
    </div>
  )
}
