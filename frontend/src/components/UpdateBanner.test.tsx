import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { UpdateBanner } from '@/components/UpdateBanner'
import { ToastProvider } from '@/components/Toast'
import type { VersionInfo } from '@/lib/api'

const version = vi.fn()
const releaseNotes = vi.fn()
const applyUpdate = vi.fn()

vi.mock('@/lib/api', () => ({
  api: {
    version: (...args: unknown[]) => version(...args),
    releaseNotes: (...args: unknown[]) => releaseNotes(...args),
    applyUpdate: (...args: unknown[]) => applyUpdate(...args),
  },
}))

function renderBanner(isAdmin = true) {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <UpdateBanner isAdmin={isAdmin} />
      </ToastProvider>
    </MemoryRouter>,
  )
}

const base: VersionInfo = {
  version: '1.2.3',
  commit: 'abc',
  built: 'now',
  schema: 7,
  latest: '1.3.0',
  update_available: true,
  update_window: {
    expires_at: '2099-01-01T00:00:00Z',
    active: true,
    is_founders: false,
    days_left: 100,
  },
}

describe('UpdateBanner', () => {
  beforeEach(() => {
    version.mockReset()
    releaseNotes.mockReset()
    applyUpdate.mockReset()
  })

  it('shows update-now state when available and window active', async () => {
    version.mockResolvedValue(base)
    renderBanner()
    expect(await screen.findByText(/v1.3.0 available/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Update now/i })).toBeInTheDocument()
  })

  it('shows renew state when available and window expired', async () => {
    version.mockResolvedValue({
      ...base,
      update_window: {
        expires_at: '2020-01-15T00:00:00Z',
        active: false,
        is_founders: false,
        days_left: -100,
      },
    })
    renderBanner()
    expect(await screen.findByText(/v1.3.0 is available/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /renew for the next version/i })).toHaveAttribute(
      'href',
      '/billing',
    )
    expect(screen.getByText(/keeps working/i)).toBeInTheDocument()
  })

  it('renders nothing when no update', async () => {
    version.mockResolvedValue({ ...base, update_available: false, latest: '1.2.3' })
    renderBanner()
    await waitFor(() => expect(version).toHaveBeenCalled())
    expect(screen.queryByText(/available/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('opens dialog and loads release notes for admins', async () => {
    const user = userEvent.setup()
    version.mockResolvedValue(base)
    releaseNotes.mockResolvedValue({
      tag: 'v1.3.0',
      published_at: '2026-04-01T00:00:00Z',
      body_markdown: '## Fixes\n- banner',
    })
    renderBanner(true)
    await user.click(await screen.findByRole('button', { name: /Update now/i }))
    expect(await screen.findByText(/banner/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Run on this host/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Copy the command/i })).toBeInTheDocument()
  })
})
