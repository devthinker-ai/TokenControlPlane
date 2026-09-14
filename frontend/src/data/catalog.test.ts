import { describe, expect, it } from 'vitest'
import {
  PROVIDER_PRESETS,
  SERVER_PRESETS,
  START_HERE_IDS,
  canAutoAdd,
  findServerPreset,
  matchProviderPreset,
  shortModelAlias,
  startHerePresets,
  suggestRouteAliases,
  type ProviderPreset,
  type ServerPreset,
} from '@/data/catalog'

const FORBIDDEN = ['placeholder', 'example.com', 'your-key', 'sk-…', 'sk-...']

function assertServerShape(p: ServerPreset) {
  expect(p.id).toBeTruthy()
  expect(p.name).toBeTruthy()
  expect(p.tagline).toBeTruthy()
  expect(['http', 'stdio']).toContain(p.transport)
  expect(typeof p.verified).toBe('boolean')
  expect(['none', 'static', 'oauth_device']).toContain(p.auth.type)

  if (p.transport === 'http') {
    expect(p.base_url, `${p.id} missing base_url`).toBeTruthy()
    if (p.verified) {
      expect(p.base_url!.startsWith('https://') || p.base_url!.startsWith('http://localhost')).toBe(
        true,
      )
    }
  } else {
    expect(p.command, `${p.id} missing command`).toBeTruthy()
    if (p.verified) {
      expect(p.command!.length).toBeGreaterThan(0)
    }
  }

  if (p.auth.type === 'static') {
    expect(p.auth.value_hint, `${p.id} static needs value_hint`).toBeTruthy()
    expect(p.auth.key_label, `${p.id} static needs key_label`).toBeTruthy()
    expect(p.auth.key_url, `${p.id} static needs key_url`).toBeTruthy()
    expect(p.auth.header || 'Authorization').toBeTruthy()
  }
}

function assertProviderShape(p: ProviderPreset) {
  expect(p.id).toBeTruthy()
  expect(p.name).toBeTruthy()
  expect(p.base_url).toBeTruthy()
  expect(p.key_label).toBeTruthy()
  expect(p.key_url).toBeTruthy()
  if (p.local) {
    expect(p.base_url.startsWith('http://localhost')).toBe(true)
  } else {
    expect(p.base_url.startsWith('https://')).toBe(true)
  }
}

function collectStrings(obj: unknown): string[] {
  if (typeof obj === 'string') return [obj]
  if (Array.isArray(obj)) return obj.flatMap(collectStrings)
  if (obj && typeof obj === 'object') {
    return Object.values(obj).flatMap(collectStrings)
  }
  return []
}

describe('catalog presets', () => {
  it('validates every ServerPreset shape', () => {
    expect(SERVER_PRESETS.length).toBeGreaterThan(10)
    for (const p of SERVER_PRESETS) assertServerShape(p)
  })

  it('validates every ProviderPreset shape', () => {
    expect(PROVIDER_PRESETS.length).toBeGreaterThan(5)
    for (const p of PROVIDER_PRESETS) assertProviderShape(p)
  })

  it('has unique ids across servers and providers', () => {
    const ids = [...SERVER_PRESETS, ...PROVIDER_PRESETS].map((p) => p.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('rejects placeholder / stub strings in catalog fields', () => {
    for (const p of [...SERVER_PRESETS, ...PROVIDER_PRESETS]) {
      const hay = collectStrings(p).join('\n').toLowerCase()
      for (const bad of FORBIDDEN) {
        expect(hay.includes(bad.toLowerCase()), `${p.id} contains "${bad}"`).toBe(false)
      }
    }
    for (const p of SERVER_PRESETS.filter((s) => s.verified)) {
      if (p.transport === 'http') expect(p.base_url).toBeTruthy()
      else expect(p.command).toBeTruthy()
    }
  })

  it('start-here set resolves and includes zero-config remotes + locals', () => {
    const cards = startHerePresets()
    expect(cards.map((c) => c.id)).toEqual([...START_HERE_IDS])
    expect(findServerPreset('deepwiki')?.auth.type).toBe('none')
    expect(findServerPreset('exa')?.auth.type).toBe('none')
    expect(findServerPreset('time')?.transport).toBe('stdio')
    expect(findServerPreset('everything')?.transport).toBe('stdio')
    expect(canAutoAdd(findServerPreset('deepwiki')!)).toBe(true)
    expect(canAutoAdd(findServerPreset('notion')!)).toBe(false)
  })

  it('matches providers to presets and suggests route aliases', () => {
    const openai = matchProviderPreset({
      name: 'OpenAI',
      base_url: 'https://api.openai.com/v1',
    })
    expect(openai?.id).toBe('openai')
    expect(openai?.models?.length).toBeGreaterThan(0)

    expect(shortModelAlias('meta-llama/Meta-Llama-3-8B-Instruct')).toBe(
      'meta-llama-3-8b-instruct',
    )
    expect(shortModelAlias('claude-sonnet-4-20250514')).toBe('claude-sonnet-4')

    const aliases = suggestRouteAliases({
      providerName: 'OpenAI',
      upstreamModel: 'gpt-4o-mini',
      existingAliases: ['chat'],
    })
    expect(aliases).not.toContain('chat')
    expect(aliases).toContain('gpt-4o-mini')
    expect(aliases).toContain('fast')
    expect(aliases.some((a) => a.startsWith('openai'))).toBe(true)
  })
})
