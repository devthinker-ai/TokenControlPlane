import type { CreateServerInput } from '@/lib/api'
import type { ServerPreset } from '@/data/catalog'

/** Build the create payload from a preset (no secrets — env values blank). */
export function createInputFromPreset(
  preset: ServerPreset,
  overrides?: Partial<{
    name: string
    authValue: string
    env: Record<string, string>
  }>,
): CreateServerInput {
  const name = overrides?.name?.trim() || preset.name

  if (preset.transport === 'stdio') {
    const env: Record<string, string> = { ...(overrides?.env || {}) }
    for (const e of preset.env || []) {
      if (!(e.key in env)) env[e.key] = ''
    }
    // Drop empty env keys so we don't send blanks unless user filled them.
    const cleaned: Record<string, string> = {}
    for (const [k, v] of Object.entries(env)) {
      if (v !== '') cleaned[k] = v
    }
    return {
      name,
      transport: 'stdio',
      command: preset.command,
      args: [...(preset.args || [])],
      env: cleaned,
      auth: { type: 'none' },
    }
  }

  if (preset.auth.type === 'none') {
    return {
      name,
      transport: 'http',
      base_url: preset.base_url,
      auth: { type: 'none' },
    }
  }

  if (preset.auth.type === 'oauth_device') {
    return {
      name,
      transport: 'http',
      base_url: preset.base_url,
      auth: { type: 'oauth_device' },
    }
  }

  const value = overrides?.authValue?.trim() || ''
  return {
    name,
    transport: 'http',
    base_url: preset.base_url,
    auth: {
      type: 'static',
      header: preset.auth.header || 'Authorization',
      value,
    },
  }
}
