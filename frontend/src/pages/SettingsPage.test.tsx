import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { SettingsPage } from '@/pages/SettingsPage'
import { ToastProvider } from '@/components/Toast'
import { initI18n, setLocale } from '@/i18n'

const me = vi.fn()
const get2FA = vi.fn()
const setup2FA = vi.fn()
const confirm2FA = vi.fn()
const delete2FA = vi.fn()
const getSMTP = vi.fn()
const getTemplates = vi.fn()

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      me: (...args: unknown[]) => me(...args),
      get2FA: (...args: unknown[]) => get2FA(...args),
      setup2FA: (...args: unknown[]) => setup2FA(...args),
      confirm2FA: (...args: unknown[]) => confirm2FA(...args),
      delete2FA: (...args: unknown[]) => delete2FA(...args),
      getSMTP: (...args: unknown[]) => getSMTP(...args),
      getTemplates: (...args: unknown[]) => getTemplates(...args),
    },
  }
})

function renderSettings() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <SettingsPage />
      </ToastProvider>
    </MemoryRouter>,
  )
}

const memberUser = {
  id: 'u1',
  email: 'm@ex.com',
  name: 'Member',
  role: 'member',
  account_id: 'a1',
  plan: 'free' as const,
  max_servers: 3,
  max_seats: 1,
}

describe('SettingsPage 2FA', () => {
  beforeEach(() => {
    initI18n()
    setLocale('en')
    me.mockReset()
    get2FA.mockReset()
    setup2FA.mockReset()
    confirm2FA.mockReset()
    delete2FA.mockReset()
    getSMTP.mockReset()
    getTemplates.mockReset()
    me.mockResolvedValue(memberUser)
  })

  it('not enrolled: setup dialog QR src starts with data:image/png; confirm shows recovery', async () => {
    const user = userEvent.setup()
    get2FA.mockResolvedValue({ enabled: false, confirmed_at: null, recovery_remaining: 0 })
    setup2FA.mockResolvedValue({
      secret: 'JBSWY3DPEHPK3PXP',
      provisioning_uri: 'otpauth://totp/MCP%20Gateway:m@ex.com?secret=JBSWY3DPEHPK3PXP',
      qr: 'data:image/png;base64,iVBORw0KGgo=',
    })
    confirm2FA.mockResolvedValue({
      recovery_codes: ['abcd-efgh', 'ijkl-mnop'],
    })
    get2FA
      .mockResolvedValueOnce({ enabled: false, confirmed_at: null, recovery_remaining: 0 })
      .mockResolvedValue({
        enabled: true,
        confirmed_at: '2026-09-10T12:00:00Z',
        recovery_remaining: 10,
      })

    renderSettings()
    expect(await screen.findByTestId('two-factor-card')).toBeInTheDocument()
    expect(screen.getByText(/not set up yet/i)).toBeInTheDocument()

    await user.click(screen.getByTestId('2fa-setup'))
    const qr = await screen.findByTestId('2fa-qr')
    expect(qr).toHaveAttribute('src', expect.stringMatching(/^data:image\/png/))

    await user.click(screen.getByTestId('2fa-i-scanned'))
    const codeInput = await screen.findByTestId('2fa-confirm-code')
    await user.type(codeInput, '123456')
    await user.click(screen.getByTestId('2fa-confirm'))

    expect(await screen.findByTestId('2fa-recovery-block')).toBeInTheDocument()
    expect(screen.getByText('abcd-efgh')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /copy all/i })).toBeInTheDocument()
  })

  it('enrolled: remove resets to not enrolled', async () => {
    const user = userEvent.setup()
    get2FA
      .mockResolvedValueOnce({
        enabled: true,
        confirmed_at: '2026-09-10T12:00:00Z',
        recovery_remaining: 7,
      })
      .mockResolvedValue({ enabled: false, confirmed_at: null, recovery_remaining: 0 })
    delete2FA.mockResolvedValue(undefined)

    renderSettings()
    expect(await screen.findByTestId('2fa-enabled')).toBeInTheDocument()
    expect(screen.getByTestId('2fa-recovery-remaining')).toHaveTextContent(/7/)

    await user.click(screen.getByTestId('2fa-remove'))
    await user.click(screen.getByTestId('2fa-remove-confirm'))

    await waitFor(() => expect(delete2FA).toHaveBeenCalled())
    expect(await screen.findByTestId('2fa-setup')).toBeInTheDocument()
    expect(screen.getByText(/not set up yet/i)).toBeInTheDocument()
  })
})
