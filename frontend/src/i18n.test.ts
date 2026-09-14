import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  getLocale,
  initI18n,
  resolveInitialLocale,
  setLocale,
  i18n,
} from '@/i18n'
import en from '@/locales/en.json'
import de from '@/locales/de.json'

function flatten(obj: Record<string, unknown>, prefix = ''): string[] {
  const keys: string[] = []
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      keys.push(...flatten(v as Record<string, unknown>, path))
    } else {
      keys.push(path)
    }
  }
  return keys
}

describe('i18n', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.lang = ''
  })

  afterEach(() => {
    void i18n.changeLanguage('en')
    document.documentElement.lang = 'en'
  })

  it('has key parity between en and de', () => {
    const enKeys = new Set(flatten(en as Record<string, unknown>))
    const deKeys = new Set(flatten(de as Record<string, unknown>))
    const onlyEn = [...enKeys].filter((k) => !deKeys.has(k)).sort()
    const onlyDe = [...deKeys].filter((k) => !enKeys.has(k)).sort()
    expect(onlyEn).toEqual([])
    expect(onlyDe).toEqual([])
  })

  it('resolves de-AT to German', () => {
    expect(resolveInitialLocale(null, 'de-AT')).toBe('de')
  })

  it('resolves en-US to English', () => {
    expect(resolveInitialLocale(null, 'en-US')).toBe('en')
  })

  it('prefers localStorage over navigator', () => {
    localStorage.setItem('tcp:lang', 'en')
    expect(resolveInitialLocale(null, 'de-DE')).toBe('en')
    localStorage.setItem('tcp:lang', 'de')
    expect(resolveInitialLocale(null, 'en-US')).toBe('de')
  })

  it('persists switcher choice and sets documentElement.lang', () => {
    initI18n()
    setLocale('de')
    expect(localStorage.getItem('tcp:lang')).toBe('de')
    expect(document.documentElement.lang).toBe('de')
    expect(getLocale()).toBe('de')

    setLocale('en')
    expect(localStorage.getItem('tcp:lang')).toBe('en')
    expect(document.documentElement.lang).toBe('en')
    expect(getLocale()).toBe('en')
  })

  it('ships 2FA placeholder keys', () => {
    expect(i18n.t('login.mfa.title')).toBeTruthy()
    expect(i18n.t('settings.twoFactor.setup')).toBeTruthy()
    void i18n.changeLanguage('de')
    expect(i18n.t('login.mfa.title')).toMatch(/Zwei-Faktor|Two-factor/i)
  })
})
