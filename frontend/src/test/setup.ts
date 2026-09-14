import '@testing-library/jest-dom/vitest'
import { initI18n, setLocale } from '@/i18n'

// jsdom lacks matchMedia — ThemeSelector / theme.ts need it.
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})

// Default tests to English chrome so existing string assertions keep working.
initI18n()
setLocale('en')
