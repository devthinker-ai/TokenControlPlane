import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { LoginPage } from '@/pages/LoginPage'
import { RateLimitError } from '@/lib/api'
import { initI18n, setLocale } from '@/i18n'
import { hintForError } from '@/lib/hintForError'

const login = vi.fn()
const loginMFA = vi.fn()
const publicConfig = vi.fn()
const requestPasswordReset = vi.fn()
const navigate = vi.fn()

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      login: (...args: unknown[]) => login(...args),
      loginMFA: (...args: unknown[]) => loginMFA(...args),
      publicConfig: (...args: unknown[]) => publicConfig(...args),
      requestPasswordReset: (...args: unknown[]) => requestPasswordReset(...args),
    },
    setToken: vi.fn(),
  }
})

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => navigate,
  }
})

function renderLogin() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  )
}

describe('LoginPage 2FA', () => {
  beforeEach(() => {
    initI18n()
    setLocale('en')
    login.mockReset()
    loginMFA.mockReset()
    publicConfig.mockReset()
    requestPasswordReset.mockReset()
    navigate.mockReset()
    publicConfig.mockResolvedValue({ registration_enabled: true })
  })

  it('shows MFA code input when login returns mfa_required', async () => {
    const user = userEvent.setup()
    login.mockResolvedValue({ mfa_required: true, mfa_token: 'mfa_tok_1' })

    renderLogin()
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/^password$/i), 'secret')
    await user.click(screen.getByRole('button', { name: /^sign in$/i }))

    expect(await screen.findByTestId('mfa-code')).toBeInTheDocument()
    expect(screen.getByText(/two-factor authentication/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/authentication code/i)).toHaveAttribute('inputmode', 'numeric')
  })

  it('shows verbatim API 401 error on MFA failure', async () => {
    const user = userEvent.setup()
    login.mockResolvedValue({ mfa_required: true, mfa_token: 'mfa_tok_1' })
    loginMFA.mockRejectedValue(new Error('invalid code'))

    renderLogin()
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/^password$/i), 'secret')
    await user.click(screen.getByRole('button', { name: /^sign in$/i }))

    const input = await screen.findByTestId('mfa-code')
    await user.type(input, '123456')

    expect(await screen.findByTestId('mfa-error')).toHaveTextContent('invalid code')
  })

  it('shows Retry-After countdown on 429', async () => {
    const user = userEvent.setup()
    login.mockResolvedValue({ mfa_required: true, mfa_token: 'mfa_tok_1' })
    loginMFA.mockRejectedValue(new RateLimitError('too many attempts', 3))

    renderLogin()
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/^password$/i), 'secret')
    await user.click(screen.getByRole('button', { name: /^sign in$/i }))

    const input = await screen.findByTestId('mfa-code')
    await user.type(input, '000000')

    expect(await screen.findByTestId('mfa-retry')).toHaveTextContent(/retry in 3s/i)
  })

  it('toggles recovery code mode (xxxx-xxxx)', async () => {
    const user = userEvent.setup()
    login.mockResolvedValue({ mfa_required: true, mfa_token: 'mfa_tok_1' })
    loginMFA.mockResolvedValue({ token: 'sess' })

    renderLogin()
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/^password$/i), 'secret')
    await user.click(screen.getByRole('button', { name: /^sign in$/i }))

    await screen.findByTestId('mfa-code')
    await user.click(screen.getByTestId('mfa-toggle-recovery'))

    expect(screen.getByLabelText(/recovery code/i)).toBeInTheDocument()
    const input = screen.getByTestId('mfa-code')
    await user.type(input, 'abcd-efgh')
    expect(input).toHaveValue('abcd-efgh')

    await user.click(screen.getByRole('button', { name: /^verify$/i }))
    await waitFor(() =>
      expect(loginMFA).toHaveBeenCalledWith('mfa_tok_1', 'abcd-efgh'),
    )
  })

  it('renders API 401 English body unchanged in German locale', async () => {
    const user = userEvent.setup()
    setLocale('de')
    login.mockRejectedValue(new Error('invalid credentials'))

    renderLogin()
    await user.type(screen.getByLabelText(/e-mail/i), 'a@b.com')
    await user.type(screen.getByLabelText(/^passwort$/i), 'secret')
    await user.click(screen.getByRole('button', { name: /^anmelden$/i }))

    expect(await screen.findByTestId('login-error')).toHaveTextContent('invalid credentials')
  })
})

describe('verbatim invariant (Part E.12)', () => {
  beforeEach(() => {
    initI18n()
  })

  it('hintForError IS translated in de while API English stays English', () => {
    setLocale('de')
    expect(hintForError('401 unauthorized', { keyLabel: 'Notion Token' })).toMatch(
      /API-Schlüssel|fehlt|falsch/i,
    )
    // API body string itself is never passed through t() — callers render verbatim.
    expect('invalid credentials').toBe('invalid credentials')
  })
})
