import { FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { api, setToken } from '@/lib/api'
import { BrandLockup } from '@/components/BrandMark'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { ThemeSelector } from '@/components/ThemeSelector'
import { LocaleSwitcher } from '@/components/LocaleSwitcher'

export function RegisterPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const inviteCode = (params.get('invite') || '').trim()
  const isJoin = inviteCode.length > 0

  const [accountName, setAccountName] = useState('')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [inviteAccount, setInviteAccount] = useState<string | null>(null)
  const [inviteLoading, setInviteLoading] = useState(isJoin)
  const [registrationEnabled, setRegistrationEnabled] = useState(true)
  const [configLoaded, setConfigLoaded] = useState(isJoin) // invite path skips open-register check

  useEffect(() => {
    if (isJoin) return
    let cancelled = false
    ;(async () => {
      try {
        const cfg = await api.publicConfig()
        if (!cancelled) setRegistrationEnabled(cfg.registration_enabled)
      } catch {
        // leave default; submit will fail closed if server disabled
      } finally {
        if (!cancelled) setConfigLoaded(true)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [isJoin])

  useEffect(() => {
    if (!isJoin) return
    let cancelled = false
    ;(async () => {
      try {
        const pub = await api.publicInvite(inviteCode)
        if (!cancelled) setInviteAccount(pub.account_name)
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('register.invalidInvite'))
          setInviteAccount(null)
        }
      } finally {
        if (!cancelled) setInviteLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [inviteCode, isJoin, t])

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      if (isJoin) {
        const res = await api.join(inviteCode, email, password, name || email)
        setToken(res.token)
      } else {
        const res = await api.register(email, password, accountName)
        setToken(res.token)
      }
      navigate('/')
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : isJoin
            ? t('register.joinFailed')
            : t('register.failed'),
      )
    } finally {
      setLoading(false)
    }
  }

  const registrationClosed = !isJoin && configLoaded && !registrationEnabled

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center bg-background p-4">
      <div className="absolute right-4 top-4 flex items-center gap-2">
        <LocaleSwitcher />
        <ThemeSelector />
      </div>
      <BrandLockup className="mb-8" />
      <Card className="w-full max-w-sm">
        <CardContent className="space-y-6 p-6">
          {registrationClosed ? (
            <>
              <div>
                <h1 className="text-lg font-semibold tracking-tight">{t('register.closedTitle')}</h1>
                <p className="mt-1 text-sm text-muted-foreground">{t('register.closedBody')}</p>
              </div>
              <Button className="w-full" onClick={() => navigate('/login')}>
                {t('register.signIn')}
              </Button>
            </>
          ) : (
            <>
              <div>
                <h1 className="text-lg font-semibold tracking-tight">
                  {isJoin
                    ? inviteLoading
                      ? t('register.checkingInvite')
                      : inviteAccount
                        ? t('register.joinTitle', { account: inviteAccount })
                        : t('register.inviteUnavailable')
                    : t('register.title')}
                </h1>
                <p className="mt-1 text-sm text-muted-foreground">
                  {isJoin ? t('register.joinSubtitle') : t('register.subtitle')}
                </p>
              </div>
              <form onSubmit={onSubmit} className="space-y-4">
                {!isJoin && (
                  <div className="space-y-1.5">
                    <Label htmlFor="account">{t('register.accountName')}</Label>
                    <Input
                      id="account"
                      value={accountName}
                      onChange={(e) => setAccountName(e.target.value)}
                      required
                    />
                  </div>
                )}
                {isJoin && (
                  <div className="space-y-1.5">
                    <Label htmlFor="name">{t('register.yourName')}</Label>
                    <Input
                      id="name"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      required
                      disabled={!inviteAccount}
                    />
                  </div>
                )}
                <div className="space-y-1.5">
                  <Label htmlFor="email">{t('common.email')}</Label>
                  <Input
                    id="email"
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    required
                    disabled={isJoin && !inviteAccount}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="password">{t('common.password')}</Label>
                  <Input
                    id="password"
                    type="password"
                    autoComplete="new-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                    minLength={8}
                    disabled={isJoin && !inviteAccount}
                  />
                </div>
                {error && (
                  <p className="flex items-center gap-1.5 text-sm text-destructive">
                    <Icon icon={AlertTriangle} className="h-3 w-3" />
                    {error}
                  </p>
                )}
                <Button
                  type="submit"
                  className="w-full"
                  disabled={loading || (isJoin && !inviteAccount)}
                >
                  {loading
                    ? isJoin
                      ? t('register.joining')
                      : t('register.creating')
                    : isJoin
                      ? t('register.join')
                      : t('register.create')}
                </Button>
              </form>
              <p className="text-center text-sm text-muted-foreground">
                {t('register.haveAccount')}{' '}
                <Link to="/login" className="font-medium text-foreground hover:underline">
                  {t('register.signIn')}
                </Link>
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
