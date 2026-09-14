import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import de from './locales/de.json'
import en from './locales/en.json'

export type AppLocale = 'de' | 'en'

const STORAGE_KEY = 'tcp:lang'

export function resolveInitialLocale(
  stored: string | null = null,
  navigatorLang: string | undefined = undefined,
): AppLocale {
  let fromStore = stored
  if (fromStore === null) {
    try {
      fromStore = localStorage.getItem(STORAGE_KEY)
    } catch {
      fromStore = null
    }
  }
  if (fromStore === 'de' || fromStore === 'en') return fromStore

  const nav =
    navigatorLang ??
    (typeof navigator !== 'undefined' ? navigator.language || navigator.languages?.[0] : undefined)
  if (nav && nav.toLowerCase().startsWith('de')) return 'de'
  return 'en'
}

/** Intl tag for the active UI language (DACH expects de-DE number/date forms). */
export function intlLocale(lng?: string): string {
  const lang = (lng ?? i18n.language ?? 'en').split('-')[0]
  return lang === 'de' ? 'de-DE' : 'en-US'
}

export function getLocale(): AppLocale {
  const lang = (i18n.language || 'en').split('-')[0]
  return lang === 'de' ? 'de' : 'en'
}

export function setLocale(lng: AppLocale): void {
  try {
    localStorage.setItem(STORAGE_KEY, lng)
  } catch {
    /* ignore */
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = lng
  }
  void i18n.changeLanguage(lng)
}

export function initI18n(): AppLocale {
  const lng = resolveInitialLocale()
  if (!i18n.isInitialized) {
    void i18n.use(initReactI18next).init({
      resources: {
        en: { translation: en },
        de: { translation: de },
      },
      lng,
      fallbackLng: 'en',
      interpolation: {
        escapeValue: false, // React already escapes
        prefix: '{{',
        suffix: '}}',
      },
    })
  } else {
    void i18n.changeLanguage(lng)
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = lng
  }
  return lng
}

export { i18n }
