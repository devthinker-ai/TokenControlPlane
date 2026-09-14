import { beforeEach, describe, expect, it } from 'vitest'
import { hintForError, hostFromUrl } from '@/lib/hintForError'
import { initI18n, setLocale, i18n } from '@/i18n'

describe('hintForError', () => {
  beforeEach(() => {
    initI18n()
    setLocale('en')
  })

  it('maps 401 / unauthorized to key hint', () => {
    expect(hintForError('401 unauthorized', { keyLabel: 'Notion integration token' })).toContain(
      'Notion integration token',
    )
    expect(hintForError('missing token')).toMatch(/API key/i)
  })

  it('maps 404 to URL path hint', () => {
    expect(hintForError('404 not found')).toMatch(/\/mcp/)
  })

  it('maps command-not-found to PATH hint', () => {
    expect(hintForError('command not found in PATH', { command: 'uvx' })).toContain('`uvx`')
  })

  it('maps timeout/network to host hint', () => {
    expect(hintForError('i/o timeout', { host: 'mcp.deepwiki.com' })).toContain('mcp.deepwiki.com')
    expect(hintForError('connection refused', { host: 'localhost:11434' })).toContain('localhost')
  })

  it('returns empty for unknown messages', () => {
    expect(hintForError('something else entirely')).toBe('')
  })

  it('hostFromUrl extracts host', () => {
    expect(hostFromUrl('https://mcp.deepwiki.com/mcp')).toBe('mcp.deepwiki.com')
  })

  it('translates hints in German while accepting optional t', () => {
    setLocale('de')
    const de = hintForError('401 unauthorized', { keyLabel: 'Notion Token' })
    expect(de).toContain('Notion Token')
    expect(de).toMatch(/API-Schlüssel|fehlt|falsch/i)

    const viaT = hintForError('404 not found', {}, (key) => i18n.t(key))
    expect(viaT).toMatch(/MCP|Pfad|\/mcp/i)
  })
})
