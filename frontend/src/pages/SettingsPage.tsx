import { FormEvent, useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Mail, Save, Shield } from 'lucide-react'
import {
  api,
  type SMTPConfig,
  type TwoFASetup,
  type TwoFAStatus,
  type User,
} from '@/lib/api'
import { fmtDateTime } from '@/lib/fmt'
import { CopyButton } from '@/components/CopyButton'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { SkeletonRows } from '@/components/ui/skeleton'

const DEFAULT_INVITE = `{{/* default invite — restore via button */}}
<!DOCTYPE html>
<html><body style="font-family:sans-serif;padding:24px;">
  <h1>You're invited to join {{.AccountName}}</h1>
  <p>{{.InviterName}} invited you. Expires {{.ExpiresAt}}.</p>
  <p><a href="{{.InviteURL}}">Accept invite</a></p>
  <p style="font-size:12px;color:#666;">{{.InviteURL}}</p>
</body></html>`

const DEFAULT_RESET = `<!DOCTYPE html>
<html><body style="font-family:sans-serif;padding:24px;">
  <h1>Reset your password</h1>
  <p>Hi {{.UserName}}, this link expires in {{.ExpiresIn}}.</p>
  <p><a href="{{.ResetURL}}">Reset password</a></p>
  <p style="font-size:12px;color:#666;">{{.ResetURL}}</p>
</body></html>`

type SetupPhase = 'qr' | 'code' | 'recovery'

export function SettingsPage() {
  const { t } = useTranslation()
  const { push } = useToast()
  const [me, setMe] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<string | null>(null)

  const [twoFA, setTwoFA] = useState<TwoFAStatus | null>(null)
  const [setupOpen, setSetupOpen] = useState(false)
  const [setupPhase, setSetupPhase] = useState<SetupPhase>('qr')
  const [setupData, setSetupData] = useState<TwoFASetup | null>(null)
  const [setupCode, setSetupCode] = useState('')
  const [setupError, setSetupError] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [showSecret, setShowSecret] = useState(false)
  const [removeOpen, setRemoveOpen] = useState(false)

  const [smtp, setSmtp] = useState<SMTPConfig>({
    enabled: false,
    host: '',
    port: 587,
    username: '',
    from: '',
    from_name: 'TokenControlPlane',
    tls_mode: 'starttls',
    has_password: false,
  })
  const [password, setPassword] = useState('')
  const [testTo, setTestTo] = useState('')
  const [testMsg, setTestMsg] = useState('')

  const [inviteTpl, setInviteTpl] = useState('')
  const [resetTpl, setResetTpl] = useState('')
  const [tplError, setTplError] = useState('')

  const isAdmin = me?.role === 'admin'

  const load = useCallback(async () => {
    setError('')
    try {
      const [user, totp] = await Promise.all([api.me(), api.get2FA()])
      setMe(user)
      setTwoFA(totp)
      if (user.role !== 'admin') return
      const [s, tpl] = await Promise.all([api.getSMTP(), api.getTemplates()])
      setSmtp(s)
      setInviteTpl(tpl.invite)
      setResetTpl(tpl.reset)
      setTestTo(user.email)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const startSetup = async () => {
    setBusy('2fa-setup')
    setSetupError('')
    setSetupCode('')
    setRecoveryCodes([])
    setShowSecret(false)
    setSetupPhase('qr')
    try {
      const data = await api.setup2FA()
      setSetupData(data)
      setSetupOpen(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  const closeSetup = () => {
    setSetupOpen(false)
    setSetupData(null)
    setSetupCode('')
    setSetupError('')
    setShowSecret(false)
    setRecoveryCodes([])
    setSetupPhase('qr')
  }

  const confirmSetup = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!setupCode.trim()) return
    setBusy('2fa-confirm')
    setSetupError('')
    try {
      const res = await api.confirm2FA(setupCode.trim())
      setRecoveryCodes(res.recovery_codes ?? [])
      setSetupPhase('recovery')
      const status = await api.get2FA()
      setTwoFA(status)
    } catch (err) {
      setSetupError(err instanceof Error ? err.message : t('settings.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  const finishSetup = () => {
    closeSetup()
    void load()
  }

  const remove2FA = async () => {
    setBusy('2fa-remove')
    try {
      await api.delete2FA()
      setRemoveOpen(false)
      setTwoFA({ enabled: false, confirmed_at: null, recovery_remaining: 0 })
      push({ title: t('settings.twoFactor.remove'), tone: 'default' })
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  const saveSMTP = async (e: FormEvent) => {
    e.preventDefault()
    setBusy('smtp')
    setTestMsg('')
    try {
      const next = await api.putSMTP({
        enabled: smtp.enabled,
        host: smtp.host,
        port: smtp.port,
        username: smtp.username,
        from: smtp.from,
        from_name: smtp.from_name,
        tls_mode: smtp.tls_mode,
        password: password || undefined,
      })
      setSmtp(next)
      setPassword('')
      push({ title: t('settings.toastSmtpSaved'), tone: 'success' })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  const sendTest = async () => {
    setBusy('test')
    setTestMsg('')
    try {
      await api.testSMTP(testTo)
      setTestMsg(t('settings.testSent'))
      push({ title: t('settings.toastTestSent'), tone: 'success' })
    } catch (err) {
      const msg = err instanceof Error ? err.message : t('settings.saveFailed')
      setTestMsg(msg)
      push({ title: msg, tone: 'danger' })
    } finally {
      setBusy(null)
    }
  }

  const saveTemplates = async () => {
    setBusy('tpl')
    setTplError('')
    try {
      const tpl = await api.putTemplates(inviteTpl, resetTpl)
      setInviteTpl(tpl.invite)
      setResetTpl(tpl.reset)
      push({ title: t('settings.toastTemplatesSaved'), tone: 'success' })
    } catch (err) {
      const msg = err instanceof Error ? err.message : t('settings.saveFailed')
      setTplError(msg)
    } finally {
      setBusy(null)
    }
  }

  const enrolled = !!twoFA?.enabled

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('settings.title')}
        description={isAdmin ? t('settings.description') : t('settings.descriptionMember')}
      />
      {error && <ErrorBanner message={error} onRetry={() => void load()} />}
      {loading && <SkeletonRows rows={4} />}

      {!loading && (
        <>
          <Card data-testid="two-factor-card">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Icon icon={Shield} className="h-4 w-4" />
                {t('settings.twoFactor.title')}
              </CardTitle>
              <CardDescription>{t('settings.twoFactor.description')}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap items-center justify-between gap-3">
              {enrolled ? (
                <>
                  <div className="space-y-1 text-sm">
                    <p className="font-medium text-foreground" data-testid="2fa-enabled">
                      {t('settings.twoFactor.enabled')}
                    </p>
                    {twoFA.confirmed_at && (
                      <p className="text-muted-foreground">
                        {t('settings.twoFactor.enabledSince', {
                          date: fmtDateTime(twoFA.confirmed_at),
                        })}
                      </p>
                    )}
                    <p className="text-muted-foreground" data-testid="2fa-recovery-remaining">
                      {t('settings.twoFactor.recoveryRemaining', {
                        count: twoFA.recovery_remaining,
                      })}
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    className="text-destructive"
                    disabled={busy !== null}
                    onClick={() => setRemoveOpen(true)}
                    data-testid="2fa-remove"
                  >
                    {t('settings.twoFactor.remove')}
                  </Button>
                </>
              ) : (
                <>
                  <p className="text-sm text-muted-foreground">{t('settings.twoFactor.notEnrolled')}</p>
                  <Button
                    type="button"
                    disabled={busy !== null}
                    onClick={() => void startSetup()}
                    data-testid="2fa-setup"
                  >
                    {busy === '2fa-setup' ? t('common.loading') : t('settings.twoFactor.setup')}
                  </Button>
                </>
              )}
            </CardContent>
          </Card>

          {isAdmin && (
            <>
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{t('settings.smtpTitle')}</CardTitle>
                  <CardDescription>{t('settings.smtpDesc')}</CardDescription>
                </CardHeader>
                <CardContent>
                  <form onSubmit={(e) => void saveSMTP(e)} className="space-y-4">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <Label htmlFor="smtp-enabled">{t('settings.enableSmtp')}</Label>
                        <p className="text-xs text-muted-foreground">{t('settings.enableSmtpHint')}</p>
                      </div>
                      <Switch
                        id="smtp-enabled"
                        checked={smtp.enabled}
                        onCheckedChange={(v) => setSmtp((s) => ({ ...s, enabled: v }))}
                      />
                    </div>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="space-y-1.5 sm:col-span-2">
                        <Label htmlFor="host">{t('settings.host')}</Label>
                        <Input
                          id="host"
                          value={smtp.host}
                          onChange={(e) => setSmtp((s) => ({ ...s, host: e.target.value }))}
                          placeholder="smtp.example.com"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="port">{t('settings.port')}</Label>
                        <Input
                          id="port"
                          type="number"
                          value={smtp.port}
                          onChange={(e) =>
                            setSmtp((s) => ({ ...s, port: Number(e.target.value) || 0 }))
                          }
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="tls">{t('settings.tlsMode')}</Label>
                        <select
                          id="tls"
                          className="flex h-9 w-full rounded-md border border-border bg-background px-3 text-sm"
                          value={smtp.tls_mode}
                          onChange={(e) => setSmtp((s) => ({ ...s, tls_mode: e.target.value }))}
                        >
                          <option value="starttls">{t('settings.tlsStarttls')}</option>
                          <option value="tls">{t('settings.tlsTls')}</option>
                          <option value="plain">{t('settings.tlsPlain')}</option>
                        </select>
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="user">{t('settings.username')}</Label>
                        <Input
                          id="user"
                          value={smtp.username}
                          onChange={(e) => setSmtp((s) => ({ ...s, username: e.target.value }))}
                          autoComplete="off"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="pass">
                          {smtp.has_password ? t('settings.passwordSet') : t('settings.password')}
                        </Label>
                        <Input
                          id="pass"
                          type="password"
                          value={password}
                          onChange={(e) => setPassword(e.target.value)}
                          placeholder={smtp.has_password ? t('settings.passwordKeep') : ''}
                          autoComplete="new-password"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="from">{t('settings.from')}</Label>
                        <Input
                          id="from"
                          value={smtp.from}
                          onChange={(e) => setSmtp((s) => ({ ...s, from: e.target.value }))}
                          placeholder="TokenControlPlane <gateway@example.com>"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label htmlFor="from_name">{t('settings.fromName')}</Label>
                        <Input
                          id="from_name"
                          value={smtp.from_name}
                          onChange={(e) => setSmtp((s) => ({ ...s, from_name: e.target.value }))}
                        />
                      </div>
                    </div>
                    <div className="flex flex-wrap items-end gap-2">
                      <Button type="submit" disabled={busy !== null}>
                        <Icon icon={Save} className="mr-1.5 h-3.5 w-3.5" />
                        {busy === 'smtp' ? t('common.saving') : t('settings.saveSmtp')}
                      </Button>
                    </div>
                  </form>

                  <div className="mt-6 space-y-2 border-t border-border pt-4">
                    <Label htmlFor="test-to">{t('settings.testEmail')}</Label>
                    <div className="flex flex-wrap gap-2">
                      <Input
                        id="test-to"
                        type="email"
                        className="max-w-xs"
                        value={testTo}
                        onChange={(e) => setTestTo(e.target.value)}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        disabled={busy !== null || !testTo}
                        onClick={() => void sendTest()}
                      >
                        <Icon icon={Mail} className="mr-1.5 h-3.5 w-3.5" />
                        {busy === 'test' ? t('settings.sending') : t('settings.sendTest')}
                      </Button>
                    </div>
                    {testMsg && <p className="text-sm text-muted-foreground">{testMsg}</p>}
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle className="text-base">{t('settings.templatesTitle')}</CardTitle>
                  <CardDescription>{t('settings.templatesDesc')}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between gap-2">
                      <Label htmlFor="tpl-invite">{t('settings.invitation')}</Label>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => setInviteTpl(DEFAULT_INVITE)}
                      >
                        {t('settings.restoreDefault')}
                      </Button>
                    </div>
                    <textarea
                      id="tpl-invite"
                      className="min-h-[180px] w-full rounded-md border border-border bg-background p-3 font-mono text-xs leading-relaxed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={inviteTpl}
                      onChange={(e) => setInviteTpl(e.target.value)}
                      spellCheck={false}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between gap-2">
                      <Label htmlFor="tpl-reset">{t('settings.passwordReset')}</Label>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => setResetTpl(DEFAULT_RESET)}
                      >
                        {t('settings.restoreDefault')}
                      </Button>
                    </div>
                    <textarea
                      id="tpl-reset"
                      className="min-h-[180px] w-full rounded-md border border-border bg-background p-3 font-mono text-xs leading-relaxed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      value={resetTpl}
                      onChange={(e) => setResetTpl(e.target.value)}
                      spellCheck={false}
                    />
                  </div>
                  {tplError && <p className="text-sm text-destructive">{tplError}</p>}
                  <Button type="button" disabled={busy !== null} onClick={() => void saveTemplates()}>
                    <Icon icon={Save} className="mr-1.5 h-3.5 w-3.5" />
                    {busy === 'tpl' ? t('common.saving') : t('settings.saveTemplates')}
                  </Button>
                </CardContent>
              </Card>
            </>
          )}
        </>
      )}

      <Dialog
        open={setupOpen}
        onClose={() => {
          if (setupPhase === 'recovery') finishSetup()
          else closeSetup()
        }}
        title={
          setupPhase === 'recovery'
            ? t('settings.twoFactor.recoveryTitle')
            : t('settings.twoFactor.setup')
        }
        description={
          setupPhase === 'qr'
            ? t('settings.twoFactor.scanQr')
            : setupPhase === 'code'
              ? t('settings.twoFactor.enterCode')
              : t('settings.twoFactor.recoveryWarning')
        }
        className="max-w-md"
      >
        {setupPhase === 'qr' && setupData && (
          <div className="space-y-4">
            <div className="flex justify-center">
              <img
                src={setupData.qr}
                alt=""
                className="h-48 w-48 rounded-md border bg-white p-2"
                data-testid="2fa-qr"
              />
            </div>
            <p className="break-all font-mono text-xs text-muted-foreground">
              {setupData.provisioning_uri}
            </p>
            <div className="space-y-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setShowSecret((v) => !v)}
              >
                {t('settings.twoFactor.showSecret')}
              </Button>
              {showSecret && (
                <div className="flex flex-wrap items-center gap-2">
                  <code
                    className="rounded border bg-secondary/50 px-2 py-1 font-mono text-sm"
                    data-testid="2fa-secret"
                  >
                    {setupData.secret}
                  </code>
                  <CopyButton text={setupData.secret} label={t('settings.twoFactor.copySecret')} />
                </div>
              )}
            </div>
            <Button
              type="button"
              className="w-full"
              onClick={() => setSetupPhase('code')}
              data-testid="2fa-i-scanned"
            >
              {t('settings.twoFactor.iScanned')}
            </Button>
          </div>
        )}

        {setupPhase === 'code' && (
          <form onSubmit={(e) => void confirmSetup(e)} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="2fa-confirm-code">{t('settings.twoFactor.enterCode')}</Label>
              <Input
                id="2fa-confirm-code"
                autoFocus
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                pattern="[0-9]*"
                value={setupCode}
                onChange={(e) => setSetupCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                data-testid="2fa-confirm-code"
              />
            </div>
            {setupError && (
              <p className="text-sm text-destructive" data-testid="2fa-setup-error">
                {setupError}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={closeSetup}>
                {t('common.cancel')}
              </Button>
              <Button
                type="submit"
                disabled={busy !== null || setupCode.length !== 6}
                data-testid="2fa-confirm"
              >
                {busy === '2fa-confirm' ? t('common.loading') : t('settings.twoFactor.confirm')}
              </Button>
            </div>
          </form>
        )}

        {setupPhase === 'recovery' && (
          <div className="space-y-4" data-testid="2fa-recovery-block">
            <p className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-foreground">
              {t('settings.twoFactor.recoveryWarning')}
            </p>
            <ul className="grid grid-cols-2 gap-1.5 font-mono text-sm">
              {recoveryCodes.map((c) => (
                <li key={c} className="rounded border bg-secondary/50 px-2 py-1">
                  {c}
                </li>
              ))}
            </ul>
            <CopyButton
              text={recoveryCodes.join('\n')}
              label={t('settings.twoFactor.copyAll')}
              className="w-full"
            />
            <Button type="button" className="w-full" onClick={finishSetup} data-testid="2fa-done">
              {t('settings.twoFactor.done')}
            </Button>
          </div>
        )}
      </Dialog>

      <Dialog
        open={removeOpen}
        onClose={() => setRemoveOpen(false)}
        title={t('settings.twoFactor.removeConfirm')}
        description={t('settings.twoFactor.removeConfirmBody')}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setRemoveOpen(false)}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="destructive"
            disabled={busy !== null}
            onClick={() => void remove2FA()}
            data-testid="2fa-remove-confirm"
          >
            {busy === '2fa-remove' ? t('common.loading') : t('settings.twoFactor.remove')}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
