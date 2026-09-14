import { FormEvent, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, Check } from 'lucide-react'
import { api } from '@/lib/api'
import { BrandLockup } from '@/components/BrandMark'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { ThemeSelector } from '@/components/ThemeSelector'
import { LocaleSwitcher } from '@/components/LocaleSwitcher'

export function ResetPasswordPage() {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const token = (params.get('token') || '').trim()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState(token ? '' : t('reset.missingToken'))
  const [done, setDone] = useState(false)
  const [loading, setLoading] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    if (password.length < 8) {
      setError(t('reset.tooShort'))
      return
    }
    if (password !== confirm) {
      setError(t('reset.mismatch'))
      return
    }
    setLoading(true)
    try {
      await api.confirmPasswordReset(token, password, confirm)
      setDone(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('reset.failed'))
    } finally {
      setLoading(false)
    }
  }

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
            <h1 className="text-lg font-semibold tracking-tight">{t('reset.title')}</h1>
            <p className="mt-1 text-sm text-muted-foreground">{t('reset.subtitle')}</p>
          </div>

          {done ? (
            <div className="space-y-4">
              <p className="flex items-center gap-1.5 text-sm text-foreground">
                <Icon icon={Check} className="h-4 w-4 text-emerald-600" />
                {t('reset.done')}
              </p>
              <Link
                to="/login"
                className="inline-flex h-9 w-full items-center justify-center rounded-md bg-primary text-sm font-medium text-primary-foreground hover:bg-primary/90"
              >
                {t('reset.backToSignIn')}
              </Link>
            </div>
          ) : (
            <form onSubmit={(e) => void onSubmit(e)} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="password">{t('reset.newPassword')}</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  minLength={8}
                  disabled={!token}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="confirm">{t('reset.confirmPassword')}</Label>
                <Input
                  id="confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  required
                  minLength={8}
                  disabled={!token}
                />
              </div>
              {error && (
                <p className="flex items-center gap-1.5 text-sm text-destructive">
                  <Icon icon={AlertTriangle} className="h-3 w-3" />
                  {error}
                </p>
              )}
              <Button type="submit" className="w-full" disabled={loading || !token}>
                {loading ? t('reset.saving') : t('reset.update')}
              </Button>
              <p className="text-center text-sm text-muted-foreground">
                <Link to="/login" className="font-medium text-foreground hover:underline">
                  {t('reset.backToSignIn')}
                </Link>
              </p>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
