import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { SetupWizard } from '@/components/SetupWizard'
import { ToastProvider } from '@/components/Toast'
import { EmptyState } from '@/components/EmptyState'
import { Server, KeyRound, CreditCard, LayoutDashboard } from 'lucide-react'

const createServer = vi.fn()
const createKey = vi.fn()
const completeOnboarding = vi.fn()
const reindexServer = vi.fn()

vi.mock('@/lib/api', () => ({
  api: {
    createServer: (...args: unknown[]) => createServer(...args),
    createKey: (...args: unknown[]) => createKey(...args),
    completeOnboarding: (...args: unknown[]) => completeOnboarding(...args),
    reindexServer: (...args: unknown[]) => reindexServer(...args),
  },
}))

function renderWizard(onDone = vi.fn(), onSkip = vi.fn()) {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <SetupWizard onDone={onDone} onSkipComplete={onSkip} />
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('SetupWizard', () => {
  beforeEach(() => {
    createServer.mockReset()
    createKey.mockReset()
    completeOnboarding.mockReset()
    reindexServer.mockReset()
    completeOnboarding.mockResolvedValue({ onboarded: true })
  })

  it('renders for a fresh account and completes onboarding', async () => {
    const user = userEvent.setup()
    const onDone = vi.fn()
    createServer.mockResolvedValue({
      id: 'srv_1',
      name: 'GitHub',
      base_url: 'https://example.com',
      enabled: true,
      tool_count: 1,
      tools: [{ name: 'search_repos' }],
      indexing_error: null,
    })
    createKey.mockResolvedValue({
      id: 'key_1',
      name: 'my-laptop',
      key: 'tcp_testkey',
      snippets: {
        claude_desktop: '{"mcpServers":{}}',
        cursor: '{"mcpServers":{}}',
        windsurf: '{"mcpServers":{}}',
      },
    })

    renderWizard(onDone)
    expect(screen.getByText(/Welcome to TokenControlPlane/i)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Set up your first server/i }))
    await user.type(screen.getByLabelText(/^Name$/i), 'GitHub')
    await user.type(screen.getByLabelText(/^URL$/i), 'https://example.com/mcp')
    await user.click(screen.getByRole('button', { name: /Add server/i }))

    await waitFor(() => expect(createServer).toHaveBeenCalled())
    expect(await screen.findByLabelText(/Key name/i)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Generate key/i }))
    expect(await screen.findByText(/Pick your client/i)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /^Next$/i }))
    await user.click(screen.getByRole('button', { name: /Go to dashboard/i }))

    await waitFor(() => expect(completeOnboarding).toHaveBeenCalled())
    expect(onDone).toHaveBeenCalled()
  })

  it('shows indexing failure with Retry and does not fake success', async () => {
    const user = userEvent.setup()
    createServer.mockResolvedValue({
      id: 'srv_fail',
      name: 'Bad',
      base_url: 'https://bad.example',
      enabled: true,
      tool_count: 0,
      tools: [],
      indexing_error: 'connection refused — is the server URL reachable?',
    })
    reindexServer.mockResolvedValue({
      id: 'srv_fail',
      tools: [],
      tool_count: 0,
      indexing_error: 'connection refused — is the server URL reachable?',
    })

    renderWizard()
    await user.click(screen.getByRole('button', { name: /Set up your first server/i }))
    await user.type(screen.getByLabelText(/^Name$/i), 'Bad')
    await user.type(screen.getByLabelText(/^URL$/i), 'https://bad.example')
    await user.click(screen.getByRole('button', { name: /Add server/i }))

    expect(await screen.findByText(/connection refused/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Retry/i })).toBeInTheDocument()
    expect(screen.queryByLabelText(/Key name/i)).not.toBeInTheDocument()
  })

  it('start-here presets never use example.com placeholders', async () => {
    const user = userEvent.setup()
    createServer.mockResolvedValue({
      id: 'srv_1',
      name: 'DeepWiki',
      base_url: 'https://mcp.deepwiki.com/mcp',
      enabled: true,
      tool_count: 1,
      tools: [{ name: 'ask' }],
      indexing_error: null,
    })
    renderWizard()
    await user.click(screen.getByRole('button', { name: /Set up your first server/i }))
    expect(await screen.findByTestId('guided-server-empty')).toBeInTheDocument()
    // Key-required: fills URL field (no auto-add)
    await user.click(screen.getByRole('button', { name: /Use Notion preset/i }))
    const url = (screen.getByLabelText(/^URL$/i) as HTMLInputElement).value
    expect(url).toBe('https://mcp.notion.com/mcp')
    expect(url).not.toMatch(/example\.com/)
    // Zero-config cards are present and labeled as presets
    expect(screen.getByRole('button', { name: /Use DeepWiki preset/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Use Exa Search preset/i })).toBeInTheDocument()
  })
})

describe('Empty states', () => {
  it('renders CTAs for the four dashboard views', () => {
    const { rerender } = render(
      <EmptyState
        icon={LayoutDashboard}
        title="No traffic yet — add a server"
        actionLabel="Go to Servers"
        onAction={() => undefined}
      />,
    )
    expect(screen.getByTestId('empty-state')).toHaveTextContent(/No traffic yet/i)

    rerender(
      <EmptyState
        icon={Server}
        title="No servers yet — add your first"
        actionLabel="Add Server"
        onAction={() => undefined}
      />,
    )
    expect(screen.getByTestId('empty-state')).toHaveTextContent(/No servers yet/i)

    rerender(
      <EmptyState
        icon={KeyRound}
        title="No API keys yet — generate one"
        actionLabel="Generate Key"
        onAction={() => undefined}
      />,
    )
    expect(screen.getByTestId('empty-state')).toHaveTextContent(/No API keys yet/i)

    rerender(
      <EmptyState
        icon={CreditCard}
        title="No billing activity yet"
        actionLabel="View plans"
        onAction={() => undefined}
      />,
    )
    expect(screen.getByTestId('empty-state')).toHaveTextContent(/No billing activity/i)
  })
})
