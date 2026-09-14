import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { initI18n, setLocale, i18n } from '@/i18n'
import { fmtBytes, fmtDate, fmtNumber, fmtTokens } from '@/lib/fmt'

describe('fmt', () => {
  beforeEach(() => {
    initI18n()
  })

  afterEach(() => {
    void i18n.changeLanguage('en')
  })

  it('fmtDate formats 2026-09-10 as de-DE and en-US', () => {
    setLocale('de')
    expect(fmtDate('2026-09-10T12:00:00.000Z')).toBe(
      new Intl.DateTimeFormat('de-DE', {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
      }).format(new Date('2026-09-10T12:00:00.000Z')),
    )
    // Explicit expectation from Phase 19 prompt (local midnight may shift UTC day —
    // use noon UTC so calendar day is stable across TZ).
    expect(fmtDate(new Date(2026, 8, 10))).toBe('10.09.2026')

    setLocale('en')
    expect(fmtDate(new Date(2026, 8, 10))).toBe('09/10/2026')
  })

  it('fmtTokens uses compact locale-aware decimals', () => {
    setLocale('en')
    expect(fmtTokens(1200)).toMatch(/1\.2K/)
    expect(fmtTokens(1_200_000)).toMatch(/1\.2M/)
    setLocale('de')
    expect(fmtTokens(1200)).toMatch(/1[,.]2K/)
  })

  it('fmtNumber and fmtBytes respect locale', () => {
    setLocale('de')
    expect(fmtNumber(1234.5)).toBe(
      new Intl.NumberFormat('de-DE').format(1234.5),
    )
    expect(fmtBytes(2048)).toMatch(/KiB/)
    expect(fmtBytes(2 * 1024 * 1024)).toMatch(/MiB/)
  })
})
