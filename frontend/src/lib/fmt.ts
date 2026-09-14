import { getLocale, i18n, intlLocale } from '@/i18n'

function activeIntl(): string {
  return intlLocale(i18n.language || getLocale())
}

/** Calendar date — e.g. 10.09.2026 (de-DE) / 09/10/2026 (en-US). */
export function fmtDate(value: Date | string | number): string {
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return String(value)
  return new Intl.DateTimeFormat(activeIntl(), {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
  }).format(d)
}

export function fmtDateTime(value: Date | string | number): string {
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return String(value)
  return new Intl.DateTimeFormat(activeIntl(), {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(d)
}

export function fmtNumber(n: number, opts?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(activeIntl(), opts).format(n)
}

/** Compact token counts: 1.2K / 1.2M with locale-aware decimals. */
export function fmtTokens(n: number): string {
  const loc = activeIntl()
  if (n >= 1_000_000) {
    return `${new Intl.NumberFormat(loc, { maximumFractionDigits: 1 }).format(n / 1_000_000)}M`
  }
  if (n >= 1_000) {
    return `${new Intl.NumberFormat(loc, { maximumFractionDigits: 1 }).format(n / 1_000)}K`
  }
  return new Intl.NumberFormat(loc).format(n)
}

/** Binary byte sizes (KiB / MiB) with locale-aware numbers. */
export function fmtBytes(n: number): string {
  const loc = activeIntl()
  if (n >= 1024 * 1024) {
    return `${new Intl.NumberFormat(loc, { maximumFractionDigits: 1 }).format(n / (1024 * 1024))} MiB`
  }
  if (n >= 1024) {
    return `${new Intl.NumberFormat(loc, { maximumFractionDigits: 1 }).format(n / 1024)} KiB`
  }
  return `${new Intl.NumberFormat(loc).format(n)} B`
}
