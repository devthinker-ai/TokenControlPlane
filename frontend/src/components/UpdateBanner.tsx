import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Copy, Loader2, X } from 'lucide-react'
import { api, type ReleaseNotes, type VersionInfo } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Icon } from '@/components/ui/icon'
import { useToast } from '@/components/Toast'
import { fmtDate } from '@/lib/fmt'

type BannerMode = 'hidden' | 'update' | 'renew'

export function UpdateBanner({ isAdmin = false }: { isAdmin?: boolean }) {
  const { t } = useTranslation()
  const { push } = useToast()
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [dismissed, setDismissed] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [notes, setNotes] = useState<ReleaseNotes | null>(null)
  const [notesErr, setNotesErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [resultMsg, setResultMsg] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const v = await api.version()
        if (!cancelled) setInfo(v)
      } catch (err) {
        console.error(err)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const mode: BannerMode = (() => {
    if (dismissed || !info?.update_available) return 'hidden'
    const win = info.update_window
    // No license / billing → treat as entitled to recommend the update (self-hosted free).
    if (!win || win.active) return 'update'
    return 'renew'
  })()

  if (mode === 'hidden' || !info) return null

  const latest = info.latest
  const current = info.version

  const openUpdateDialog = async () => {
    setDialogOpen(true)
    setNotes(null)
    setNotesErr('')
    setResultMsg('')
    try {
      setNotes(await api.releaseNotes(latest))
    } catch (err) {
      setNotesErr(err instanceof Error ? err.message : t('update.notesFailed'))
    }
  }

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText('tokencontrolplane update')
      push({ title: t('update.toastCopied'), tone: 'default' })
    } catch {
      push({ title: t('common.copyFailed'), tone: 'danger' })
    }
  }

  const runUpdate = async () => {
    setBusy(true)
    setResultMsg('')
    try {
      const res = await api.applyUpdate()
      const msg =
        res.restart === 'systemd'
          ? res.message || t('update.restartSystemd')
          : `${res.message || t('update.restartManual')}`
      setResultMsg(msg)
      push({ title: t('update.toastApplied'), tone: 'default' })
    } catch (err) {
      const m = err instanceof Error ? err.message : t('update.toastFailed')
      setResultMsg(m)
      push({ title: m, tone: 'danger' })
    } finally {
      setBusy(false)
    }
  }

  if (mode === 'renew') {
    const ended = info.update_window?.expires_at
      ? fmtDate(info.update_window.expires_at)
      : 'recently'
    return (
      <div
        className="flex items-center justify-between gap-3 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-sm"
        role="status"
      >
        <p className="min-w-0 text-foreground">
          <span className="font-medium">{t('update.renewAvailable', { latest })}</span>
          <span className="text-muted-foreground">
            {t('update.windowEnded', { ended })}
            <Link
              to="/billing"
              className="font-medium text-foreground underline-offset-2 hover:underline"
            >
              {t('update.renewLink')}
            </Link>
            {t('update.stayOn', { current })}
          </span>
        </p>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t('update.dismissAria')}
          onClick={() => setDismissed(true)}
        >
          {t('common.dismiss')}
          <Icon icon={X} className="ml-1 h-3.5 w-3.5" />
        </Button>
      </div>
    )
  }

  return (
    <>
      <div
        className="flex items-center justify-between gap-3 border-b border-border bg-primary/8 px-4 py-2 text-sm"
        role="status"
      >
        <p className="min-w-0 text-foreground">
          <span className="font-medium">{t('update.available', { latest })}</span>
          <span className="text-muted-foreground">{t('update.onVersion', { current })}</span>
          <button
            type="button"
            className="font-medium text-foreground underline-offset-2 hover:underline"
            onClick={() => void openUpdateDialog()}
          >
            {t('update.updateNow')}
          </button>
        </p>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t('update.dismissAria')}
          onClick={() => setDismissed(true)}
        >
          {t('common.dismiss')}
          <Icon icon={X} className="ml-1 h-3.5 w-3.5" />
        </Button>
      </div>

      <Dialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        title={t('update.dialogTitle', { latest })}
        description={t('update.dialogDesc', { current })}
        className="max-w-xl"
      >
        <div className="max-h-64 overflow-auto rounded-md border border-border bg-secondary/40 p-3">
          {notesErr && <p className="text-sm text-destructive">{notesErr}</p>}
          {!notesErr && !notes && (
            <p className="text-sm text-muted-foreground">{t('update.loadingNotes')}</p>
          )}
          {notes && (
            <pre className="whitespace-pre-wrap font-sans text-sm text-foreground">
              {notes.body_markdown || t('update.noNotes')}
            </pre>
          )}
        </div>
        {resultMsg && (
          <p className="mt-3 text-sm text-muted-foreground" role="status">
            {resultMsg}
          </p>
        )}
        <div className="mt-4 flex flex-wrap gap-2">
          {isAdmin && (
            <Button type="button" disabled={busy} onClick={() => void runUpdate()}>
              {busy && <Icon icon={Loader2} className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              {t('update.runHost')}
            </Button>
          )}
          <Button type="button" variant="secondary" onClick={() => void copyCommand()}>
            <Icon icon={Copy} className="mr-1.5 h-3.5 w-3.5" />
            {t('update.copyCommand')}
          </Button>
          <Button type="button" variant="ghost" onClick={() => setDialogOpen(false)}>
            {t('common.close')}
          </Button>
        </div>
        {!isAdmin && (
          <p className="mt-2 text-xs text-muted-foreground">
            {t('update.askAdmin')}
          </p>
        )}
      </Dialog>
    </>
  )
}
