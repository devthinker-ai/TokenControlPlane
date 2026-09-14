import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { clearToken, api, type OnboardingState, type User, type VersionInfo } from '@/lib/api'
import { cn, formatRelative } from '@/lib/utils'
import { fmtDate, fmtDateTime } from '@/lib/fmt'
import { i18n } from '@/i18n'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'
import { Icon } from '@/components/ui/icon'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { BrandLockup, BrandMark } from '@/components/BrandMark'
import { ActivityBell } from '@/components/ActivityBell'
import { LocaleSwitcher } from '@/components/LocaleSwitcher'
import { SetupWizard } from '@/components/SetupWizard'
import { UpdateBanner } from '@/components/UpdateBanner'
import { useToast } from '@/components/Toast'
import { ThemeCycleButton, ThemeSelector } from '@/components/ThemeSelector'
import {
  LayoutDashboard,
  Server,
  Cpu,
  KeyRound,
  CreditCard,
  LogOut,
  Settings,
  Activity,
  PanelLeftClose,
  PanelLeftOpen,
  Users,
  Copy,
  Check,
  ArrowUpCircle,
} from 'lucide-react'

const navAll = [
  { to: '/', labelKey: 'nav.overview', icon: LayoutDashboard },
  { to: '/servers', labelKey: 'nav.servers', icon: Server },
  { to: '/providers', labelKey: 'nav.providers', icon: Cpu },
  { to: '/keys', labelKey: 'nav.keys', icon: KeyRound },
  { to: '/members', labelKey: 'nav.members', icon: Users, adminOnly: true },
  { to: '/settings', labelKey: 'nav.settings', icon: Settings },
  { to: '/activity', labelKey: 'nav.activity', icon: Activity },
  { to: '/billing', labelKey: 'nav.billing', icon: CreditCard, adminOnly: true },
]

const COLLAPSE_KEY = 'tcp_sidebar_collapsed'

export function AppLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { push } = useToast()
  const [onboarding, setOnboarding] = useState<OnboardingState | null>(null)
  const [forceWizard, setForceWizard] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const [user, setUser] = useState<User | null>(null)
  const [version, setVersion] = useState<VersionInfo | null>(null)
  const [versionOpen, setVersionOpen] = useState(false)
  const [versionCopied, setVersionCopied] = useState(false)
  const [changePwOpen, setChangePwOpen] = useState(false)
  const [pwCurrent, setPwCurrent] = useState('')
  const [pwNew, setPwNew] = useState('')
  const [pwConfirm, setPwConfirm] = useState('')
  const [pwBusy, setPwBusy] = useState(false)
  const [pwError, setPwError] = useState('')
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSE_KEY) === '1'
    } catch {
      return false
    }
  })

  const toggleCollapsed = () => {
    setCollapsed((v) => {
      const next = !v
      try {
        localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0')
      } catch {
        /* ignore */
      }
      return next
    })
    setMenuOpen(false)
  }

  const refreshOnboarding = useCallback(async () => {
    try {
      setOnboarding(await api.onboarding())
    } catch (err) {
      console.error(err)
    }
  }, [])

  useEffect(() => {
    void refreshOnboarding()
    void api.me().then(setUser).catch(console.error)
    api
      .version()
      .then(setVersion)
      .catch(() =>
        setVersion({
          version: 'dev',
          commit: '',
          built: '',
          schema: 0,
          latest: 'dev',
          update_available: false,
          update_window: null,
        }),
      )
  }, [refreshOnboarding])

  const logout = () => {
    clearToken()
    navigate('/login')
  }

  const showWizard =
    forceWizard ||
    (onboarding !== null && !onboarding.onboarded && !onboarding.has_servers)

  const nav = navAll.filter((item) => !item.adminOnly || user?.role === 'admin')

  const closeWizard = async () => {
    setForceWizard(false)
    await refreshOnboarding()
  }

  const initials = (user?.name || user?.email || '?')
    .split(/\s+/)
    .map((p) => p[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-background md:flex-row">
      <aside
        className={cn(
          'flex w-full shrink-0 flex-col border-b border-border bg-background transition-[width] duration-200 ease-out md:h-full md:border-b-0 md:border-r',
          collapsed ? 'md:w-14' : 'md:w-60',
        )}
      >
        <div
          className={cn(
            'flex items-center px-4 py-4',
            collapsed ? 'md:flex-col md:gap-2 md:px-2' : 'justify-between md:px-3',
          )}
        >
          {/* Mobile: always full lockup. Desktop: mark only when collapsed. */}
          <div className="md:hidden">
            <BrandLockup />
          </div>
          <div className="hidden md:block">
            {collapsed ? <BrandMark /> : <BrandLockup />}
          </div>

          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="hidden md:inline-flex"
              aria-label={collapsed ? t('layout.expandSidebar') : t('layout.collapseSidebar')}
              title={collapsed ? t('layout.expandSidebar') : t('layout.collapseSidebar')}
              onClick={toggleCollapsed}
            >
              <Icon icon={collapsed ? PanelLeftOpen : PanelLeftClose} />
            </Button>
            <div className="md:hidden">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('layout.accountMenu')}
                onClick={() => setMenuOpen((v) => !v)}
              >
                <Icon icon={Settings} />
              </Button>
            </div>
          </div>
        </div>

        <nav
          className={cn(
            'flex gap-0.5 overflow-x-auto pb-3 md:min-h-0 md:flex-1 md:flex-col md:overflow-y-auto md:overflow-x-visible',
            collapsed ? 'md:items-center md:px-2' : 'px-3',
          )}
        >
          {nav.map(({ to, labelKey, icon }) => {
            const label = t(labelKey)
            return (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              title={label}
              aria-label={label}
              className={({ isActive }) =>
                cn(
                  'relative flex h-9 shrink-0 items-center gap-2 rounded-md text-sm font-medium transition-colors',
                  collapsed ? 'md:w-9 md:justify-center md:px-0' : 'px-2.5',
                  isActive
                    ? 'bg-secondary text-foreground'
                    : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                )
              }
            >
              {({ isActive }) => (
                <>
                  {isActive && !collapsed && (
                    <span
                      className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-full bg-primary"
                      aria-hidden
                    />
                  )}
                  {isActive && collapsed && (
                    <span
                      className="absolute left-0 top-1.5 bottom-1.5 hidden w-0.5 rounded-full bg-primary md:block"
                      aria-hidden
                    />
                  )}
                  <Icon icon={icon} className="h-4 w-4 shrink-0" />
                  <span className={cn(collapsed && 'md:hidden')}>{label}</span>
                </>
              )}
            </NavLink>
          )})}
        </nav>

        <div
          className={cn(
            'relative z-20 hidden border-t border-border md:block',
            collapsed ? 'p-2' : 'p-3',
          )}
        >
          {collapsed && (
            <div className="mb-2 flex justify-center">
              <ThemeCycleButton />
            </div>
          )}
          <button
            type="button"
            className={cn(
              'group flex items-center rounded-md text-left hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
              collapsed
                ? 'mx-auto h-9 w-9 justify-center'
                : 'w-full gap-2.5 px-2 py-2',
            )}
            onClick={() => setMenuOpen((v) => !v)}
            aria-label={t('layout.accountMenu')}
            title={user?.email || t('layout.account')}
          >
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-secondary text-xs font-medium tabular-nums">
              {initials}
            </span>
            {!collapsed && (
              <>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">
                    {user?.name || t('layout.account')}
                  </span>
                  <span className="block truncate text-xs text-muted-foreground">
                    {user?.email || ''}
                  </span>
                </span>
                <Icon
                  icon={Settings}
                  className="h-3.5 w-3.5 opacity-0 transition-opacity group-hover:opacity-100"
                />
              </>
            )}
          </button>
          {menuOpen && (
            <div
              className={cn(
                'absolute bottom-full mb-1 rounded-lg border border-border bg-card p-1 shadow-md',
                collapsed ? 'left-full ml-2 w-52' : 'left-3 right-3',
              )}
            >
              <div className="px-2 py-2">
                <p className="mb-1.5 text-xs font-medium text-muted-foreground">
                  {t('common.theme')}
                </p>
                <ThemeSelector />
              </div>
              <div className="my-1 border-t border-border" />
              <button
                type="button"
                className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-secondary"
                onClick={() => {
                  setMenuOpen(false)
                  setChangePwOpen(true)
                  setPwCurrent('')
                  setPwNew('')
                  setPwConfirm('')
                  setPwError('')
                }}
              >
                {t('layout.changePassword')}
              </button>
              <button
                type="button"
                className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-secondary"
                onClick={() => {
                  setMenuOpen(false)
                  setForceWizard(true)
                  push({ title: t('layout.replayingTour'), tone: 'default' })
                }}
              >
                {t('layout.replayTour')}
              </button>
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-secondary"
                onClick={logout}
              >
                <Icon icon={LogOut} className="h-3.5 w-3.5" />
                {t('layout.logOut')}
              </button>
            </div>
          )}
        </div>

        <button
          type="button"
          className={cn(
            'text-center text-[10px] tabular-nums text-muted-foreground/70 transition-colors hover:text-muted-foreground',
            collapsed ? 'mt-1 hidden w-full md:block' : 'w-full px-3 pb-2 pt-1',
          )}
          title={
            version
              ? `tokencontrolplane v${version.version} · schema ${version.schema}`
              : 'Version'
          }
          onClick={() => setVersionOpen(true)}
        >
          {version ? `v${version.version} · schema ${version.schema}` : ''}
        </button>

        {menuOpen && (
          <div className="space-y-1 border-t border-border p-2 md:hidden">
            <div className="px-2 py-1">
              <p className="mb-1.5 text-xs font-medium text-muted-foreground">
                {t('common.theme')}
              </p>
              <ThemeSelector />
            </div>
            <button
              type="button"
              className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-secondary"
              onClick={() => {
                setMenuOpen(false)
                setChangePwOpen(true)
                setPwCurrent('')
                setPwNew('')
                setPwConfirm('')
                setPwError('')
              }}
            >
              {t('layout.changePassword')}
            </button>
            <button
              type="button"
              className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-secondary"
              onClick={() => {
                setMenuOpen(false)
                setForceWizard(true)
              }}
            >
              {t('layout.replayTour')}
            </button>
            <button
              type="button"
              className="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-secondary"
              onClick={logout}
            >
              {t('layout.logOut')}
            </button>
          </div>
        )}
      </aside>

      <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto">
        <UpdateBanner isAdmin={user?.role === 'admin'} />
        <div className="sticky top-0 z-20 flex items-center justify-end gap-1 border-b border-border bg-background/95 px-6 py-2 backdrop-blur supports-[backdrop-filter]:bg-background/80 lg:px-8">
          <LocaleSwitcher />
          <ActivityBell align="end" side="bottom" />
        </div>
        <div className="mx-auto w-full max-w-[1200px] flex-1 p-6 lg:p-8">
          <Outlet />
        </div>
      </main>

      <Dialog
        open={versionOpen}
        onClose={() => {
          setVersionOpen(false)
          setVersionCopied(false)
        }}
        title={t('layout.aboutTitle')}
        description={t('layout.aboutDesc')}
        className="max-w-md"
      >
        <VersionDetails
          version={version}
          copied={versionCopied}
          onCopy={async () => {
            const text = [
              `tokencontrolplane v${version?.version ?? '?'}`,
              `commit ${formatCommit(version?.commit)}`,
              `built ${version?.built || '?'}`,
              `schema ${version?.schema ?? '?'}`,
            ].join(' · ')
            try {
              await navigator.clipboard.writeText(text)
              setVersionCopied(true)
              push({ title: t('layout.copiedDetails'), tone: 'default' })
              window.setTimeout(() => setVersionCopied(false), 2000)
            } catch {
              push({ title: t('common.copyFailed'), tone: 'danger' })
            }
          }}
        />
      </Dialog>

      <Dialog
        open={changePwOpen}
        onClose={() => setChangePwOpen(false)}
        title={t('layout.changePassword')}
        description={t('layout.changePasswordDesc')}
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            void (async () => {
              setPwError('')
              setPwBusy(true)
              try {
                await api.changeMyPassword(pwCurrent, pwNew, pwConfirm)
                setChangePwOpen(false)
                push({ title: t('layout.passwordUpdated'), tone: 'success' })
              } catch (err) {
                setPwError(err instanceof Error ? err.message : t('common.updateFailed'))
              } finally {
                setPwBusy(false)
              }
            })()
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="pw-current">{t('layout.currentPassword')}</Label>
            <Input
              id="pw-current"
              type="password"
              autoComplete="current-password"
              value={pwCurrent}
              onChange={(e) => setPwCurrent(e.target.value)}
              required
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="pw-new">{t('layout.newPassword')}</Label>
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              value={pwNew}
              onChange={(e) => setPwNew(e.target.value)}
              required
              minLength={8}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="pw-confirm">{t('layout.confirm')}</Label>
            <Input
              id="pw-confirm"
              type="password"
              autoComplete="new-password"
              value={pwConfirm}
              onChange={(e) => setPwConfirm(e.target.value)}
              required
              minLength={8}
            />
          </div>
          {pwError && <p className="text-sm text-destructive">{pwError}</p>}
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={() => setChangePwOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pwBusy}>
              {pwBusy ? t('common.saving') : t('layout.update')}
            </Button>
          </div>
        </form>
      </Dialog>

      {showWizard && (
        <SetupWizard
          onDone={() => void closeWizard()}
          onSkipComplete={() => void closeWizard()}
        />
      )}
    </div>
  )
}

function formatCommit(commit: string | undefined): string {
  if (!commit || commit === 'none') return 'local'
  return commit.length > 12 ? commit.slice(0, 7) : commit
}

function formatBuilt(built: string | undefined): string {
  if (!built || built === 'unknown' || built === 'none') return i18n.t('layout.notRecorded')
  const parsed = Date.parse(built)
  if (!Number.isNaN(parsed)) {
    return fmtDateTime(parsed)
  }
  return built
}

function VersionDetails({
  version,
  copied,
  onCopy,
}: {
  version: VersionInfo | null
  copied: boolean
  onCopy: () => void
}) {
  const { t } = useTranslation()
  const ver = version?.version || '…'
  const isDev = !version || ver === 'dev' || ver === 'vdev' || ver.startsWith('dev')
  const updateAvailable = Boolean(version?.update_available)
  const windowActive = !version?.update_window || version.update_window.active
  const status = (() => {
    if (!version) return { label: t('layout.loading'), variant: 'secondary' as const }
    if (updateAvailable && windowActive) {
      return {
        label: t('layout.updateAvailable', { latest: version.latest }),
        variant: 'default' as const,
      }
    }
    if (updateAvailable && !windowActive) {
      return { label: t('layout.renewToInstall'), variant: 'outline' as const }
    }
    if (isDev) return { label: t('layout.devBuild'), variant: 'secondary' as const }
    return { label: t('layout.upToDate'), variant: 'secondary' as const }
  })()

  const rows: { label: string; value: string; mono?: boolean; hint?: string }[] = [
    {
      label: t('layout.schema'),
      value: version ? String(version.schema) : '…',
      hint: t('layout.schemaHint'),
    },
    {
      label: t('layout.commit'),
      value: formatCommit(version?.commit),
      mono: true,
    },
    {
      label: t('layout.built'),
      value: formatBuilt(version?.built),
      hint:
        version?.built && !Number.isNaN(Date.parse(version.built))
          ? formatRelative(version.built)
          : undefined,
    },
  ]

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-border bg-secondary/30 px-4 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t('layout.version')}
            </p>
            <p className="mt-1 font-mono text-2xl font-semibold tracking-tight tabular-nums">
              v{ver}
            </p>
          </div>
          <Badge variant={status.variant} className="mt-0.5">
            {updateAvailable && windowActive ? (
              <Icon icon={ArrowUpCircle} className="mr-1 h-3 w-3" />
            ) : null}
            {status.label}
          </Badge>
        </div>
        {version?.update_window ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {version.update_window.active
              ? t('layout.windowOpen', {
                  count: version.update_window.days_left,
                  days: version.update_window.days_left,
                })
              : t('layout.windowEnded', {
                  date: fmtDate(version.update_window.expires_at),
                })}
            {version.update_window.is_founders ? t('layout.foundersSuffix') : ''}
          </p>
        ) : null}
      </div>

      <dl className="divide-y divide-border rounded-lg border border-border">
        {rows.map((row) => (
          <div
            key={row.label}
            className="flex items-baseline justify-between gap-4 px-3.5 py-2.5 text-sm"
          >
            <dt className="text-muted-foreground">
              {row.label}
              {row.hint ? (
                <span className="mt-0.5 block text-[11px] text-muted-foreground/80">
                  {row.hint}
                </span>
              ) : null}
            </dt>
            <dd
              className={cn(
                'text-right font-medium',
                row.mono && 'font-mono text-xs tabular-nums',
              )}
              title={row.hint}
            >
              {row.value}
            </dd>
          </div>
        ))}
      </dl>

      <Button type="button" variant="secondary" className="w-full" onClick={() => void onCopy()}>
        <Icon icon={copied ? Check : Copy} className="mr-1.5 h-3.5 w-3.5" />
        {copied ? t('common.copied') : t('layout.copyDetails')}
      </Button>
    </div>
  )
}
