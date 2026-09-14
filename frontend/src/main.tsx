import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/inter/index.css'
import './index.css'
import App from './App'
import { initTheme } from '@/lib/theme'
import { initI18n } from '@/i18n'

// Apply stored theme / locale before paint to avoid a flash.
initTheme()
initI18n()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
