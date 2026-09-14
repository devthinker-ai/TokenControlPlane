import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy, KeyRound, Mail, UserPlus, Users } from 'lucide-react'
import {
  api,
  type CreateInviteResponse,
  type PendingInvite,
  type TeamUser,
  type User,
} from '@/lib/api'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { EmptyState } from '@/components/EmptyState'
import { useToast } from '@/components/Toast'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SkeletonRows } from '@/components/ui/skeleton'
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
import { formatDate, formatRelative } from '@/lib/utils'

export function MembersPage() {
  const { t } = useTranslation()
  const { push } = useToast()
  const [me, setMe] = useState<User | null>(null)
  const [users, setUsers] = useState<TeamUser[]>([])
  const [seatsUsed, setSeatsUsed] = useState(0)
  const [seatsMax, setSeatsMax] = useState(0)
  const [invites, setInvites] = useState<PendingInvite[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [created, setCreated] = useState<CreateInviteResponse | null>(null)
  const [inviteMode, setInviteMode] = useState<'choose' | 'email' | null>(null)
  const [inviteEmail, setInviteEmail] = useState('')
  const [emailSentNote, setEmailSentNote] = useState('')
  const [removeId, setRemoveId] = useState<string | null>(null)
  const [resetUser, setResetUser] = useState<TeamUser | null>(null)
  const [generatedPassword, setGeneratedPassword] = useState<string | null>(null)
  const [resetEmailSent, setResetEmailSent] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      const [user, team, pending] = await Promise.all([
        api.me(),
        api.listUsers(),
        api.listInvites(),
      ])
      setMe(user)
      setUsers(team.users)
      setSeatsUsed(team.seats_used)
      setSeatsMax(team.seats_max)
      setInvites(pending)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const createInviteLink = async () => {
    setBusy('invite')
    setEmailSentNote('')
    try {
      const inv = await api.createInvite()
      setCreated(inv)
      setInviteMode(null)
      await load()
      push({ title: t('members.toastInviteCreated'), tone: 'success' })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.inviteFailed'))
    } finally {
      setBusy(null)
    }
  }

  const createInviteEmail = async () => {
    setBusy('invite-email')
    setEmailSentNote('')
    try {
      const res = await api.createEmailInvite(inviteEmail.trim())
      setCreated(res.invite)
      setInviteMode(null)
      setInviteEmail('')
      if (res.email_sent) {
        setEmailSentNote(t('members.toastEmailSent'))
        push({ title: t('members.toastInviteEmailed'), tone: 'success' })
      } else {
        setEmailSentNote(res.email_error || t('members.toastEmailFailedNote'))
        push({
          title: res.email_error || t('members.toastEmailFailed'),
          tone: 'danger',
        })
      }
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.inviteEmailFailed'))
    } finally {
      setBusy(null)
    }
  }

  const revoke = async (id: string) => {
    setBusy(`rev-${id}`)
    try {
      await api.deleteInvite(id)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.revokeFailed'))
    } finally {
      setBusy(null)
    }
  }

  const setRole = async (id: string, role: 'admin' | 'member') => {
    setBusy(`role-${id}`)
    try {
      await api.patchUserRole(id, role)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.roleFailed'))
    } finally {
      setBusy(null)
    }
  }

  const remove = async () => {
    if (!removeId) return
    setBusy(`del-${removeId}`)
    try {
      await api.deleteUser(removeId)
      setRemoveId(null)
      await load()
      push({ title: t('members.toastRemoved'), tone: 'default' })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.removeFailed'))
    } finally {
      setBusy(null)
    }
  }

  const doReset = async () => {
    if (!resetUser) return
    setBusy(`reset-${resetUser.id}`)
    try {
      const res = await api.adminResetPassword(resetUser.id)
      setResetEmailSent(!!res.email_sent)
      setGeneratedPassword(res.new_password || null)
      push({
        title: res.email_sent ? t('members.toastResetEmailed') : t('members.toastReset'),
        tone: 'success',
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('members.resetFailed'))
      setResetUser(null)
    } finally {
      setBusy(null)
    }
  }

  const inviteLink = created
    ? created.url || `${window.location.origin}${created.path}`
    : ''

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('members.title')}
        description={t('members.seatsUsed', { used: seatsUsed, max: seatsMax })}
        actions={
          <Button onClick={() => setInviteMode('choose')} disabled={busy !== null}>
            <Icon icon={UserPlus} className="mr-1.5 h-3.5 w-3.5" />
            {t('members.newInvite')}
          </Button>
        }
      />

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}
      {loading && <SkeletonRows rows={3} />}

      {!loading && (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('members.name')}</TableHead>
                <TableHead>{t('members.email')}</TableHead>
                <TableHead>{t('members.role')}</TableHead>
                <TableHead className="hidden sm:table-cell">{t('members.joined')}</TableHead>
                <TableHead className="text-right">{t('members.actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && <TableLoading colSpan={5} />}
              {!loading && users.length === 0 && (
                <TableEmpty colSpan={5} icon={Users} title={t('members.noMembers')} />
              )}
              {users.map((u) => {
                const isSelf = me?.id === u.id
                return (
                  <TableRow key={u.id}>
                    <TableCell className="font-medium">
                      <span className="inline-flex flex-wrap items-center gap-1.5">
                        {u.name}
                        {u.totp_enabled && (
                          <Badge
                            variant="secondary"
                            className="text-[10px] font-medium"
                            data-testid="totp-badge"
                          >
                            {t('members.totpBadge')}
                          </Badge>
                        )}
                      </span>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{u.email}</TableCell>
                    <TableCell>
                      <Badge variant={u.role === 'admin' ? 'default' : 'secondary'}>
                        {u.role}
                      </Badge>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground sm:table-cell">
                      {formatDate(u.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex flex-wrap justify-end gap-1">
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={busy !== null}
                          title={t('members.resetPassword')}
                          onClick={() => {
                            setResetUser(u)
                            setGeneratedPassword(null)
                            setResetEmailSent(false)
                          }}
                        >
                          <Icon icon={KeyRound} className="mr-1 h-3.5 w-3.5" />
                          {t('members.reset')}
                        </Button>
                        {u.role === 'member' ? (
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={busy !== null}
                            onClick={() => void setRole(u.id, 'admin')}
                          >
                            {t('members.promote')}
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={busy !== null || isSelf}
                            title={isSelf ? t('members.cannotDemoteSelf') : undefined}
                            onClick={() => void setRole(u.id, 'member')}
                          >
                            {t('members.demote')}
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          className="text-destructive"
                          disabled={busy !== null || isSelf}
                          title={isSelf ? t('members.cannotRemoveSelf') : t('members.remove')}
                          onClick={() => setRemoveId(u.id)}
                        >
                          Remove
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="space-y-3">
        <h2 className="text-sm font-semibold tracking-tight">{t('members.pendingInvites')}</h2>
        {!loading && invites.length === 0 && (
          <EmptyState
            icon={Users}
            title={t('members.noPending')}
            description={t('members.noPendingDesc')}
          />
        )}
        {invites.length > 0 && (
          <ul className="divide-y rounded-lg border">
            {invites.map((inv) => (
              <li
                key={inv.id}
                className="flex flex-wrap items-center justify-between gap-2 px-3 py-2.5 text-sm"
              >
                <div>
                  <span className="font-medium">{inv.role}</span>
                  <span className="text-muted-foreground">
                    {' '}
                    · {t('members.byAdmin', { name: inv.created_by_name || 'admin' })} · {t('members.expires', { when: formatRelative(inv.expires_at) })}
                  </span>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy !== null}
                  onClick={() => void revoke(inv.id)}
                >
                  {t('members.revoke')}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <Dialog
        open={inviteMode === 'choose'}
        onClose={() => setInviteMode(null)}
        title={t('members.inviteChooseTitle')}
        description={t('members.inviteChooseDesc')}
      >
        <div className="flex flex-col gap-2">
          <Button
            variant="outline"
            className="justify-start"
            disabled={busy !== null}
            onClick={() => void createInviteLink()}
          >
            <Icon icon={Copy} className="mr-2 h-4 w-4" />
            {busy === 'invite' ? t('members.creating') : t('members.copyLink')}
          </Button>
          <Button
            variant="outline"
            className="justify-start"
            disabled={busy !== null}
            onClick={() => setInviteMode('email')}
          >
            <Icon icon={Mail} className="mr-2 h-4 w-4" />
            {t('members.sendByEmail')}
          </Button>
        </div>
      </Dialog>

      <Dialog
        open={inviteMode === 'email'}
        onClose={() => setInviteMode(null)}
        title={t('members.emailInviteTitle')}
        description={t('members.emailInviteDesc')}
      >
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="invite-email">{t('members.recipientEmail')}</Label>
            <Input
              id="invite-email"
              type="email"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              placeholder={t('members.recipientPlaceholder')}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setInviteMode('choose')}>
              {t('common.back')}
            </Button>
            <Button
              disabled={busy !== null || !inviteEmail.trim()}
              onClick={() => void createInviteEmail()}
            >
              {busy === 'invite-email' ? t('members.sending') : t('members.sendInvite')}
            </Button>
          </div>
        </div>
      </Dialog>

      <Dialog
        open={!!created}
        onClose={() => {
          setCreated(null)
          setEmailSentNote('')
        }}
        title={t('members.inviteLinkTitle')}
        description={t('members.inviteLinkDesc')}
      >
        <div className="space-y-3">
          {emailSentNote && (
            <p className="text-sm text-muted-foreground">{emailSentNote}</p>
          )}
          <pre className="overflow-x-auto rounded-md border bg-secondary/50 p-3 font-mono text-xs break-all">
            {inviteLink}
          </pre>
          <Button
            className="w-full"
            onClick={() => {
              void navigator.clipboard.writeText(inviteLink)
              push({ title: t('members.toastCopiedInvite'), tone: 'success' })
            }}
          >
            <Icon icon={Copy} className="mr-1.5 h-3.5 w-3.5" />
            {t('members.copyLink')}
          </Button>
        </div>
      </Dialog>

      <Dialog
        open={!!removeId}
        onClose={() => setRemoveId(null)}
        title={t('members.removeTitle')}
        description={t('members.removeDesc')}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setRemoveId(null)}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={() => void remove()} disabled={busy !== null}>
            {t('members.remove')}
          </Button>
        </div>
      </Dialog>

      <Dialog
        open={!!resetUser && !generatedPassword}
        onClose={() => setResetUser(null)}
        title={t('members.resetTitle')}
        description={
          resetUser
            ? t('members.resetDesc', { name: resetUser.name, email: resetUser.email })
            : undefined
        }
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setResetUser(null)}>
            Cancel
          </Button>
          <Button onClick={() => void doReset()} disabled={busy !== null}>
            {busy?.startsWith('reset') ? t('members.resetting') : t('members.resetPassword')}
          </Button>
        </div>
      </Dialog>

      <Dialog
        open={!!generatedPassword}
        onClose={() => {
          setGeneratedPassword(null)
          setResetUser(null)
        }}
        title={t('members.newPasswordTitle')}
        description={
          resetEmailSent
            ? t('members.newPasswordEmailed')
            : t('members.newPasswordOnce')
        }
      >
        <div className="space-y-3">
          <pre className="overflow-x-auto rounded-md border bg-secondary/50 p-3 font-mono text-sm">
            {generatedPassword}
          </pre>
          <Button
            className="w-full"
            onClick={() => {
              if (generatedPassword) {
                void navigator.clipboard.writeText(generatedPassword)
                push({ title: t('members.toastCopiedPassword'), tone: 'success' })
              }
            }}
          >
            <Icon icon={Copy} className="mr-1.5 h-3.5 w-3.5" />
            {t('members.copyPassword')}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
