import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { ServersPage } from '@/pages/ServersPage'
import { ToastProvider } from '@/components/Toast'
import { canAutoAdd, findServerPreset } from '@/data/catalog'
import { createInputFromPreset } from '@/lib/createInputFromPreset'

const listServers = vi.fn()
const createServer = vi.fn()
const me = vi.fn()
const serverStatus = vi.fn()
const checkCommand = vi.fn()
const parseMCPJSON = vi.fn()

vi.mock('@/lib/api', () => ({
  api: {
    listServers: (...a: unknown[]) => listServers(...a),
    createServer: (...a: unknown[]) => createServer(...a),
    me: (...a: unknown[]) => me(...a),
    serverStatus: (...a: unknown[]) => serverStatus(...a),
    checkCommand: (...a: unknown[]) => checkCommand(...a),
    parseMCPJSON: (...a: unknown[]) => parseMCPJSON(...a),
    listServerTools: vi.fn(),
    patchServer: vi.fn(),
    deleteServer: vi.fn(),
    reindexServer: vi.fn(),
    connectServer: vi.fn(),
    reconnectServer: vi.fn(),
    connectStatus: vi.fn(),
  },
}))

function renderPage() {
  return render(
    <MemoryRouter>
      <ToastProvider>
        <ServersPage />
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('ServersPage presets', () => {
  beforeEach(() => {
    listServers.mockReset()
    createServer.mockReset()
    me.mockReset()
    serverStatus.mockReset()
    checkCommand.mockReset()
    listServers.mockResolvedValue([])
    me.mockResolvedValue({ role: 'admin', tool_policy: false })
    checkCommand.mockResolvedValue({ found: true })
  })

  it('selecting deepwiki fills Advanced form (http, none auth)', async () => {
    const user = userEvent.setup()
    renderPage()
    expect(await screen.findByTestId('guided-server-empty')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Use DeepWiki preset/i }))

    expect(await screen.findByTestId('advanced-server-form')).toBeInTheDocument()
    expect(screen.getByLabelText(/^Name$/i)).toHaveValue('DeepWiki')
    expect(screen.getByLabelText(/Base URL/i)).toHaveValue('https://mcp.deepwiki.com/mcp')
    expect(screen.getByRole('tab', { name: /None/i })).toHaveAttribute('aria-selected', 'true')

    const expected = createInputFromPreset(findServerPreset('deepwiki')!)
    expect(expected.transport).toBe('http')
    expect(expected.auth).toEqual({ type: 'none' })
  })

  it('selecting fs fills stdio command + args', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('guided-server-empty')

    await user.click(screen.getByRole('button', { name: /Browse all presets/i }))
    expect(await screen.findByTestId('quick-add-grid')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Use Filesystem preset/i }))

    expect(await screen.findByTestId('advanced-server-form')).toBeInTheDocument()
    expect(screen.getByLabelText(/^Command$/i)).toHaveValue('npx')
    expect(screen.getByLabelText(/Args/i)).toHaveValue(
      '-y\n@modelcontextprotocol/server-filesystem\n~/Desktop',
    )
    expect(createInputFromPreset(findServerPreset('fs')!).args?.[0]).toBe('-y')
  })

  it('key-required card lands on Advanced with key focused, does not auto-submit', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByTestId('guided-server-empty')

    expect(canAutoAdd(findServerPreset('notion')!)).toBe(false)
    await user.click(screen.getByRole('button', { name: /Use Notion preset/i }))

    expect(await screen.findByTestId('advanced-server-form')).toBeInTheDocument()
    expect(createServer).not.toHaveBeenCalled()
    const keyInput = screen.getByLabelText(/Notion integration token/i)
    expect(keyInput).toBeInTheDocument()
    await waitFor(() => expect(keyInput).toHaveFocus())
  })

  it('Add now on zero-config card creates server and clears empty state', async () => {
    const user = userEvent.setup()
    const created = {
      id: 'srv_dw',
      name: 'DeepWiki',
      base_url: 'https://mcp.deepwiki.com/mcp',
      enabled: true,
      transport: 'http',
      tool_count: 2,
      tools: [{ name: 'ask_question' }, { name: 'read_wiki' }],
      indexing_error: null,
    }
    createServer.mockResolvedValue(created)
    listServers
      .mockResolvedValueOnce([])
      .mockResolvedValue([{ ...created, tools: undefined }])
    serverStatus.mockResolvedValue({ state: 'closed' })

    renderPage()
    await screen.findByTestId('guided-server-empty')

    const card = screen.getByTestId('preset-card-deepwiki')
    await user.click(within(card).getByRole('button', { name: /Add DeepWiki now/i }))

    await waitFor(() => expect(createServer).toHaveBeenCalled())
    expect(createServer).toHaveBeenCalledWith(createInputFromPreset(findServerPreset('deepwiki')!))

    await waitFor(() => {
      expect(screen.queryByTestId('guided-server-empty')).not.toBeInTheDocument()
    })
    expect(await screen.findByText('DeepWiki')).toBeInTheDocument()
  })
})
