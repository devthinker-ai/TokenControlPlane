import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Copy, CreditCard, Eye, EyeOff } from 'lucide-react'
import {
  api,
  PRICING_FALLBACK,
  type LicenseInfo,
  type Plan,
  type PricingPlan,
  type PricingResponse,
  type User,
} from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Icon } from '@/components/ui/icon'
import { SkeletonRows } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/EmptyState'
import { ErrorBanner, PageHeader } from '@/components/PageHeader'
import { cn, formatTokens } from '@/lib/utils'
import { fmtDate } from '@/lib/fmt'

function daysUntil(iso: string): number {
  return Math.ceil((new Date(iso).getTime() - Date.now()) / 86400000)
}

export function BillingPage() {
  const { t } = useTranslation()
  const [user, setUser] = useState<User | null>(null)
  const [lic, setLic] = useState<LicenseInfo | null>(null)
  const [licMissing, setLicMissing] = useState(false)
  const [pricing, setPricing] = useState<PricingResponse>(PRICING_FALLBACK)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [revealed, setRevealed] = useState(false)
  const [reissueOpen, setReissueOpen] = useState(false)
  const [reissueEmail, setReissueEmail] = useState('')

  const load = async () => {
    const [me, price] = await Promise.all([
      api.me(),
      api.pricing().catch(() => PRICING_FALLBACK),
    ])
    setUser(me)
    setPricing(price)
    try {
      setLic(await api.myLicense())
      setLicMissing(false)
    } catch {
      setLic(null)
      setLicMissing(true)
    }
  }

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        await load()
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('billing.loadFailed'))
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const buy = async (plan: Plan) => {
    setBusy(`buy-${plan}`)
    setError('')
    try {
      const { url } = await api.buy(plan)
      window.location.href = url
    } catch (err) {
      setError(err instanceof Error ? err.message : t('billing.checkoutFailed'))
      setBusy(null)
    }
  }

  const portal = async () => {
    setBusy('portal')
    setError('')
    try {
      const { url } = await api.billingPortal()
      window.open(url, '_blank', 'noopener,noreferrer')
    } catch (err) {
      setError(err instanceof Error ? err.message : t('billing.portalFailed'))
    } finally {
      setBusy(null)
    }
  }

  const reissue = async () => {
    setBusy('reissue')
    setError('')
    try {
      const { key } = await api.reissueLicense(reissueEmail || undefined)
      setLic((prev) => (prev ? { ...prev, key, needs_manual_key: false } : prev))
      setRevealed(true)
      setReissueOpen(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('billing.reissueFailed'))
    } finally {
      setBusy(null)
    }
  }

  const copy = async (text: string) => {
    await navigator.clipboard.writeText(text)
  }

  const active = lic && !lic.revoked
  const expired = Boolean(active && lic.expired)
  const soon = Boolean(active && !expired && lic.expires_at && daysUntil(lic.expires_at) <= 14)
  const masked =
    lic?.key && lic.key.length > 24
      ? `${lic.key.slice(0, 12)}…${lic.key.slice(-8)}`
      : lic?.key ?? ''

  const envLine = `TOKENCONTROLPLANE_LICENSE_KEY=${revealed && lic ? lic.key : '<your-license-key>'}`
  const dockerBlock = `environment:\n  TOKENCONTROLPLANE_LICENSE_KEY: "${revealed && lic ? lic.key : '<your-license-key>'}"`
  const windowMo = lic?.window_months ?? 12
  const renewPlan: Plan = renewPlanFromLicense(lic?.plan)
  const planLabel =
    lic?.plan === 'team' ? t('billing.planTeam') : t('billing.planPro')

  return (
    <div className="space-y-6">
      <PageHeader
        title={t('billing.title')}
        description={t('billing.description')}
      />

      {error && <ErrorBanner message={error} onRetry={() => void load()} />}
      {loading && <SkeletonRows rows={2} />}

      {!loading && soon && lic && (
        <div className="rounded-md border border-amber-500/30 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-400">
          {t('billing.soonBanner', { date: fmtDate(lic.expires_at) })}{' '}
          <strong className="font-semibold">{t('billing.keepsWorking')}</strong> {t('billing.renewNext')}{' '}
          <button
            type="button"
            className="underline underline-offset-2"
            onClick={() => void buy(renewPlan)}
          >
            {t('billing.buyAgain')}
          </button>
        </div>
      )}
      {!loading && expired && lic && (
        <div className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
          {t('billing.expiredBanner')}{' '}
          <strong className="font-semibold text-foreground">
            {t('billing.fullyFunctional')}
          </strong>{' '}
          {t('billing.resumeUpdates')}{' '}
          <button
            type="button"
            className="underline underline-offset-2 text-foreground"
            onClick={() => void buy(renewPlan)}
          >
            {t('billing.buyAgain')}
          </button>
        </div>
      )}

      {!loading && !user && !error && (
        <EmptyState
          icon={CreditCard}
          title={t('billing.emptyTitle')}
          description={t('billing.emptyDesc')}
        />
      )}

      {!loading && user && licMissing && (
        <div className="space-y-8">
          <PricingCards
            pricing={pricing}
            busy={busy}
            onBuy={(plan) => void buy(plan)}
          />
          <div className="mx-auto max-w-xl space-y-1 text-center">
            <p className="text-sm text-muted-foreground">{t('billing.guarantee')}</p>
            <p className="text-xs text-muted-foreground">{t('billing.refund')}</p>
          </div>
        </div>
      )}

      {!loading && user && active && lic && (
        <div className="grid max-w-2xl gap-4">
          <Card>
            <CardHeader
              action={
                soon || expired ? (
                  <Button size="sm" onClick={() => void buy(renewPlan)}>
                    {t('billing.renew')}
                  </Button>
                ) : undefined
              }
            >
              <CardTitle>{t('billing.yourKey')}</CardTitle>
              <CardDescription className="flex flex-wrap items-center gap-2 pt-1">
                <Badge variant={expired ? 'secondary' : 'default'}>{lic.plan}</Badge>
                {lic.is_founders && <Badge variant="outline">{t('billing.founders')}</Badge>}
                <span
                  className={cn(
                    'text-sm',
                    soon && 'text-amber-600 dark:text-amber-400',
                    expired && 'text-muted-foreground',
                  )}
                >
                  {expired
                    ? t('billing.updatesPaused')
                    : t('billing.updatesUntil', {
                        plan: planLabel,
                        date: fmtDate(lic.expires_at),
                        months: windowMo,
                      })}
                  {!expired && lic.is_founders ? t('billing.foundersExtra') : ''}
                </span>
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-sm text-muted-foreground">
                {t('billing.seatsUsed', { used: user.seats_used ?? '—', max: user.max_seats })}
              </p>
              <p className="text-sm text-muted-foreground">{t('billing.guarantee')}</p>
              <div className="rounded-md border bg-secondary/50 p-3 font-mono text-sm break-all">
                {revealed ? lic.key : masked}
              </div>
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" onClick={() => setRevealed((v) => !v)}>
                  <Icon icon={revealed ? EyeOff : Eye} className="mr-1 h-3.5 w-3.5" />
                  {revealed ? t('billing.hide') : t('billing.reveal')}
                </Button>
                <Button variant="outline" size="sm" onClick={() => void copy(lic.key)}>
                  <Icon icon={Copy} className="mr-1 h-3.5 w-3.5" />
                  {t('common.copy')}
                </Button>
              </div>

              <div>
                <p className="mb-2 text-sm font-semibold">{t('billing.useIt')}</p>
                <div className="grid gap-3 sm:grid-cols-2">
                  <SnippetBlock label={t('billing.envVar')} value={envLine} onCopy={() => void copy(envLine)} />
                  <SnippetBlock
                    label={t('billing.dockerCompose')}
                    value={dockerBlock}
                    onCopy={() => void copy(dockerBlock)}
                  />
                </div>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  onClick={() => void buy(renewPlan)}
                  disabled={busy !== null}
                >
                  {t('billing.buyAgainFresh')}
                </Button>
                <Button
                  variant="ghost"
                  onClick={() => {
                    setReissueEmail(user.email)
                    setReissueOpen(true)
                  }}
                  disabled={busy !== null}
                >
                  {t('billing.reissueKey')}
                </Button>
                <Button variant="ghost" onClick={() => void portal()} disabled={busy !== null}>
                  {t('billing.myPurchases')}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      <Dialog
        open={reissueOpen}
        onClose={() => setReissueOpen(false)}
        title={t('billing.reissueTitle')}
        description={t('billing.reissueDesc')}
      >
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="reissue-email">{t('billing.emailSubject')}</Label>
            <Input
              id="reissue-email"
              type="email"
              value={reissueEmail}
              onChange={(e) => setReissueEmail(e.target.value)}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setReissueOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => void reissue()} disabled={busy === 'reissue'}>
              {busy === 'reissue' ? t('billing.reissuing') : t('billing.reissueKey')}
            </Button>
          </div>
        </div>
      </Dialog>
    </div>
  )
}

function PricingCards({
  pricing,
  busy,
  onBuy,
}: {
  pricing: PricingResponse
  busy: string | null
  onBuy: (plan: Plan) => void
}) {
  const founders = pricing.founders_available
  return (
    <div className="grid max-w-5xl gap-5 pt-3 sm:grid-cols-2">
      {pricing.plans.map((p) => (
        <PlanCard key={p.key} plan={p} founders={founders} busy={busy} onBuy={onBuy} />
      ))}
    </div>
  )
}

/** Bullet lines for a pricing card — exported for tests. */
export function planCardBullets(plan: PricingPlan): string[] {
  const tokens =
    plan.caps.monthly_tokens === 0
      ? 'Unlimited tokens'
      : `${formatTokens(plan.caps.monthly_tokens)} tokens/mo`
  return [
    `${plan.caps.max_servers} servers · ${plan.caps.max_seats} seats`,
    tokens,
    'Self-hosted — your data stays on your server',
    'Named-team flat key (not per-seat billing)',
  ]
}

/** Resolve renew checkout plan from an active license plan string. */
export function renewPlanFromLicense(plan: string | undefined): Plan {
  if (plan === 'team') return 'team'
  return 'pro'
}

function PlanCard({
  plan,
  founders,
  busy,
  onBuy,
}: {
  plan: PricingPlan
  founders: boolean
  busy: string | null
  onBuy: (plan: Plan) => void
}) {
  const { t } = useTranslation()
  const recommended = plan.key === 'team'
  const displayPrice = founders ? plan.founders_price : plan.price
  const showListPrice = founders && plan.price !== displayPrice
  const windowMo = founders ? plan.founders_window : plan.window_months
  return (
    <Card
      className={cn(
        'relative flex h-full flex-col',
        recommended && 'ring-1 ring-primary/30',
      )}
    >
      {recommended ? (
        <Badge className="absolute -top-2.5 left-1/2 z-10 -translate-x-1/2 whitespace-nowrap">
          {t('billing.recommended')}
        </Badge>
      ) : null}
      <CardHeader className={cn(recommended && 'pt-6')}>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="text-base">{plan.label}</CardTitle>
          {founders ? <Badge variant="outline">Founders</Badge> : null}
        </div>
        <div className="flex items-baseline gap-2 pt-3">
          <p className="text-3xl font-semibold tabular-nums tracking-tight">${displayPrice}</p>
          {showListPrice ? (
            <span className="text-sm tabular-nums text-muted-foreground line-through">
              ${plan.price}
            </span>
          ) : null}
        </div>
        <CardDescription>{t('billing.oneTime')}</CardDescription>
        <p className="text-sm text-muted-foreground">
          {t('billing.monthsUpdates', { months: windowMo })}{founders ? t('billing.foundersOffer') : ''}
        </p>
      </CardHeader>
      <CardContent className="mt-auto flex flex-1 flex-col space-y-4">
        <ul className="space-y-2 text-sm">
          {planCardBullets(plan).map((b) => (
            <li key={b} className="flex gap-2">
              <Icon icon={Check} className="mt-0.5 h-3.5 w-3.5 shrink-0 text-primary" />
              <span>{b}</span>
            </li>
          ))}
        </ul>
        <Button
          className="mt-auto w-full"
          variant={recommended ? 'default' : 'outline'}
          onClick={() => onBuy(plan.key)}
          disabled={busy !== null}
        >
          {busy === `buy-${plan.key}` ? t('billing.redirecting') : t('billing.buy')}
        </Button>
      </CardContent>
    </Card>
  )
}

function SnippetBlock({
  label,
  value,
  onCopy,
}: {
  label: string
  value: string
  onCopy: () => void
}) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        <Button variant="ghost" size="icon" aria-label={`Copy ${label}`} onClick={onCopy}>
          <Icon icon={Copy} className="h-3.5 w-3.5" />
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-md border bg-secondary/50 p-2 font-mono text-xs whitespace-pre-wrap">
        {value}
      </pre>
    </div>
  )
}
