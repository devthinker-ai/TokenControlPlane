import { clsx, type ClassValue } from 'clsx'
import { i18n } from '@/i18n'
import { fmtDate, fmtDateTime, fmtTokens } from '@/lib/fmt'

export function cn(...inputs: ClassValue[]) {
  return clsx(inputs)
}

/** @deprecated Prefer fmtTokens from @/lib/fmt */
export function formatTokens(n: number): string {
  return fmtTokens(n)
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return i18n.t('common.never')
  try {
    return fmtDateTime(iso)
  } catch {
    return iso
  }
}

/** Relative time with absolute ISO in title attribute usage. */
export function formatRelative(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return i18n.t('common.never').toLowerCase()
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const diff = Math.round((now - t) / 1000)
  if (diff < 0) return i18n.t('common.justNow')
  if (diff < 60) return i18n.t('common.secondsAgo', { count: diff })
  const m = Math.floor(diff / 60)
  if (m < 60) return i18n.t('common.minutesAgo', { count: m })
  const h = Math.floor(m / 60)
  if (h < 24) return i18n.t('common.hoursAgo', { count: h })
  const d = Math.floor(h / 24)
  if (d < 30) return i18n.t('common.daysAgo', { count: d })
  return fmtDate(iso)
}

export function dayLabel(iso: string, now = new Date()): string {
  const d = new Date(iso)
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const day = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const delta = Math.round((today.getTime() - day.getTime()) / 86400000)
  if (delta === 0) return i18n.t('common.today')
  if (delta === 1) return i18n.t('common.yesterday')
  return fmtDate(iso)
}
