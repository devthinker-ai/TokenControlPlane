import { describe, expect, it } from 'vitest'
import { findServerPreset } from '@/data/catalog'
import { createInputFromPreset } from '@/lib/createInputFromPreset'

describe('createInputFromPreset', () => {
  it('deepwiki → http none', () => {
    const p = findServerPreset('deepwiki')!
    const input = createInputFromPreset(p)
    expect(input).toEqual({
      name: 'DeepWiki',
      transport: 'http',
      base_url: 'https://mcp.deepwiki.com/mcp',
      auth: { type: 'none' },
    })
  })

  it('fs → stdio npx filesystem args', () => {
    const p = findServerPreset('fs')!
    const input = createInputFromPreset(p)
    expect(input.transport).toBe('stdio')
    expect(input.command).toBe('npx')
    expect(input.args).toEqual([
      '-y',
      '@modelcontextprotocol/server-filesystem',
      '~/Desktop',
    ])
    expect(input.auth).toEqual({ type: 'none' })
  })

  it('notion static includes auth value override', () => {
    const p = findServerPreset('notion')!
    const input = createInputFromPreset(p, { authValue: 'Bearer secret' })
    expect(input.auth).toEqual({
      type: 'static',
      header: 'Authorization',
      value: 'Bearer secret',
    })
  })

  it('oauth_device for zapier', () => {
    const p = findServerPreset('zapier')!
    expect(createInputFromPreset(p).auth).toEqual({ type: 'oauth_device' })
  })
})
