import { FormEvent, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { api, isMFARequired, RateLimitError, setToken } from '@/lib/api'
import { BrandLockup } from '@/components/BrandMark'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { ThemeSelector } from '@/components/ThemeSelector'
import { LocaleSwitcher } from '@/components/LocaleSwitcher'

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [forgot, setForgot] = useState(false)
  const [forgotSent, setForgotSent] = useState(false)
  const [forgotEmail, setForgotEmail] = useState('')
  const [registrationEnabled, setRegistrationEnabled] = useState(true)

  const [mfaToken, setMfaToken] = useState<string | null>(null)
  const [mfaCode, setMfaCode] = useState('')
  const [useRecovery, setUseRecovery] = useState(false)
  const [retryAfter, setRetryAfter] = useState(0)
  const mfaSubmitting = useRef(false)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const cfg = await api.publicConfig()
        if (!cancelled) setRegistrationEnabled(cfg.registration_enabled)
      } catch {
        // Default open if meta unreachable — register endpoint still enforces.
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (retryAfter <= 0) return
    const id = window.setInterval(() => {
      setRetryAfter((s) => (s <= 1 ? 0 : s - 1))
    }, 1000)
    return () => window.clearInterval(id)
  }, [retryAfter])

  const finishLogin = (token: string) => {
    setToken(token)
    navigate('/')
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await api.login(email, password)
      if (isMFARequired(res)) {
        setMfaToken(res.mfa_token)
        setMfaCode('')
        setUseRecovery(false)
        setRetryAfter(0)
        return
      }
      finishLogin(res.token)
    } catch (err) {
      // API errors stay verbatim (machine-facing).
      setError(err instanceof Error ? err.message : t('login.failed'))
    } finally {
      setLoading(false)
    }
  }

  const submitMFA = async (code: string) => {
    if (!mfaToken || mfaSubmitting.current || retryAfter > 0) return
    const trimmed = code.trim()
    if (!trimmed) return
    mfaSubmitting.current = true
    setError('')
    setLoading(true)
    try {
      const res = await api.loginMFA(mfaToken, trimmed)
      finishLogin(res.token)
    } catch (err) {
      if (err instanceof RateLimitError) {
        setRetryAfter(err.retryAfter > 0 ? err.retryAfter : 60)
        setError(err.message)
      } else {
        setError(err instanceof Error ? err.message : t('login.failed'))
      }
    } finally {
      setLoading(false)
      mfaSubmitting.current = false
    }
  }

  const onMFASubmit = async (e: FormEvent) => {
    e.preventDefault()
    await submitMFA(mfaCode)
  }

  const onMfaCodeChange = (value: string) => {
    if (useRecovery) {
      // Allow digits/letters + hyphen for xxxx-xxxx recovery codes.
      const cleaned = value.replace(/[^a-zA-Z0-9-]/g, '').slice(0, 9)
      setMfaCode(cleaned)
      return
    }
    const digits = value.replace(/\D/g, '').slice(0, 6)
    setMfaCode(digits)
    if (digits.length === 6) {
      void submitMFA(digits)
    }
  }

  const cancelMFA = () => {
    setMfaToken(null)
    setMfaCode('')
    setUseRecovery(false)
    setError('')
    setRetryAfter(0)
  }

  const onForgot = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await api.requestPasswordReset(forgotEmail || email)
      setForgotSent(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('login.requestFailed'))
    } finally {
      setLoading(false)
    }
  }

  const mfaStep = mfaToken !== null

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center bg-background p-4">
      <div className="absolute right-4 top-4 flex items-center gap-2">
        <LocaleSwitcher />
        <ThemeSelector />
      </div>
      <BrandLockup className="mb-8" />
      <Card className="w-full max-w-sm">
        <CardContent className="space-y-6 p-6">
          <div>
            <h1 className="text-lg font-semibold tracking-tight">
              {forgot
                ? t('login.forgotTitle')
                : mfaStep
                  ? t('login.mfa.title')
                  : t('login.title')}
            </h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {forgot
                ? t('login.forgotSubtitle')
                : mfaStep
                  ? t('login.mfa.subtitle')
                  : t('login.subtitle')}
            </p>
          </div>

          {forgot ? (
            forgotSent ? (
              <div className="space-y-4">
                <p className="text-sm text-muted-foreground">{t('login.forgotSent')}</p>
                <Button
                  type="button"
                  variant="outline"
                  className="w-full"
                  onClick={() => {
                    setForgot(false)
                    setForgotSent(false)
                  }}
                >
                  {t('login.backToSignIn')}
                </Button>
              </div>
            ) : (
              <form onSubmit={(e) => void onForgot(e)} className="space-y-4">
                <div className="space-y-1.5">
                  <Label htmlFor="forgot-email">{t('common.email')}</Label>
                  <Input
                    id="forgot-email"
                    type="email"
                    autoComplete="email"
                    value={forgotEmail || email}
                    onChange={(e) => setForgotEmail(e.target.value)}
                    required
                  />
                </div>
                {error && (
                  <p className="flex items-center gap-1.5 text-sm text-destructive">
                    <Icon icon={AlertTriangle} className="h-3 w-3" />
                    {error}
                  </p>
                )}
                <Button type="submit" className="w-full" disabled={loading}>
                  {loading ? t('login.sending') : t('login.sendReset')}
                </Button>
                <button
                  type="button"
                  className="w-full text-center text-sm text-muted-foreground hover:text-foreground"
                  onClick={() => {
                    setForgot(false)
                    setError('')
                  }}
                >
                  {t('login.backToSignIn')}
                </button>
              </form>
            )
          ) : mfaStep ? (
            <form onSubmit={(e) => void onMFASubmit(e)} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="mfa-code">
                  {useRecovery ? t('login.mfa.recoveryLabel') : t('login.mfa.codeLabel')}
                </Label>
                <Input
                  id="mfa-code"
                  autoFocus
                  inputMode={useRecovery ? 'text' : 'numeric'}
                  autoComplete="one-time-code"
                  pattern={useRecovery ? undefined : '[0-9]*'}
                  maxLength={useRecovery ? 9 : 6}
                  placeholder={useRecovery ? 'xxxx-xxxx' : '000000'}
                  value={mfaCode}
                  onChange={(e) => onMfaCodeChange(e.target.value)}
                  disabled={loading || retryAfter > 0}
                  required
                  data-testid="mfa-code"
                />
              </div>
              {error && (
                <p
                  className="flex items-center gap-1.5 text-sm text-destructive"
                  data-testid="mfa-error"
                >
                  <Icon icon={AlertTriangle} className="h-3 w-3" />
                  {error}
                </p>
              )}
              {retryAfter > 0 && (
                <p className="text-sm text-muted-foreground" data-testid="mfa-retry">
                  {t('login.mfa.retryIn', { seconds: retryAfter })}
                </p>
              )}
              <Button
                type="submit"
                className="w-full"
                disabled={loading || retryAfter > 0 || !mfaCode.trim()}
              >
                {loading ? t('login.mfa.verifying') : t('login.mfa.verify')}
              </Button>
              <button
                type="button"
                className="w-full text-center text-sm text-muted-foreground hover:text-foreground"
                onClick={() => {
                  setUseRecovery((v) => !v)
                  setMfaCode('')
                  setError('')
                }}
                data-testid="mfa-toggle-recovery"
              >
                {useRecovery ? t('login.mfa.useTotp') : t('login.mfa.useRecovery')}
              </button>
              <button
                type="button"
                className="w-full text-center text-sm text-muted-foreground hover:text-foreground"
                onClick={cancelMFA}
                data-testid="mfa-cancel"
              >
                {t('login.mfa.cancel')}
              </button>
            </form>
          ) : (
            <>
              <form onSubmit={(e) => void onSubmit(e)} className="space-y-4">
                <div className="space-y-1.5">
                  <Label htmlFor="email">{t('common.email')}</Label>
                  <Input
                    id="email"
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    required
                  />
                </div>
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label htmlFor="password">{t('common.password')}</Label>
                    <button
                      type="button"
                      className="text-xs font-medium text-muted-foreground hover:text-foreground"
                      onClick={() => {
                        setForgot(true)
                        setForgotEmail(email)
                        setError('')
                      }}
                    >
                      {t('login.forgotLink')}
                    </button>
                  </div>
                  <Input
                    id="password"
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                  />
                </div>
                {error && (
                  <p
                    className="flex items-center gap-1.5 text-sm text-destructive"
                    data-testid="login-error"
                  >
                    <Icon icon={AlertTriangle} className="h-3 w-3" />
                    {error}
                  </p>
                )}
                <Button type="submit" className="w-full" disabled={loading}>
                  {loading ? t('login.signingIn') : t('login.signIn')}
                </Button>
              </form>
              <p className="text-center text-sm text-muted-foreground">
                {registrationEnabled ? (
                  <>
                    {t('login.noAccount')}{' '}
                    <Link to="/register" className="font-medium text-foreground hover:underline">
                      {t('login.createAccount')}
                    </Link>
                  </>
                ) : (
                  t('login.registrationClosed')
                )}
              </p>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
