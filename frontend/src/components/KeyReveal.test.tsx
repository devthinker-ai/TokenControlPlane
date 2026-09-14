import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { KeyReveal } from './KeyReveal'

describe('KeyReveal', () => {
  it('shows the plaintext key once with a shown-only-once warning', () => {
    render(
      <KeyReveal
        plaintextKey="tcp_testkey123"
        snippets={{
          claude_desktop: '{"mcpServers":{}}',
          cursor: '{"mcpServers":{}}',
          windsurf: '{"mcpServers":{}}',
        }}
      />,
    )

    expect(screen.getByText(/shown only once/i)).toBeInTheDocument()
    expect(screen.getByText('tcp_testkey123')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /copy key/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /claude desktop/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /cursor/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /windsurf/i })).toBeInTheDocument()
  })
})
