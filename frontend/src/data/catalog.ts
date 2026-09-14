/**
 * Embedded preset catalog — ships inside the SPA (air-gap friendly).
 * TODO(phase17+): optional GET /api/v1/catalog returning the same JSON so
 * the catalog can be updated server-side without a frontend rebuild.
 * Do not implement that endpoint in this phase.
 */

export type ServerCategory =
  | 'search'
  | 'dev'
  | 'docs'
  | 'data'
  | 'productivity'
  | 'browser'
  | 'local'

export type ServerPreset = {
  id: string
  name: string
  tagline: string
  category: ServerCategory
  transport: 'http' | 'stdio'
  verified: boolean
  base_url?: string
  auth: {
    type: 'none' | 'static' | 'oauth_device'
    header?: string
    value_hint?: string
    key_label?: string
    key_url?: string
  }
  command?: string
  args?: string[]
  env?: { key: string; value_hint: string }[]
  needs_local?: boolean
}

export type ProviderPreset = {
  id: string
  name: string
  tagline: string
  base_url: string
  default_model?: string
  /** Popular upstream model ids for this provider (route dialog chips). */
  models?: string[]
  key_label: string
  key_url: string
  local?: boolean
}

export const PROVIDER_PRESETS: ProviderPreset[] = [
  {
    id: 'openai',
    name: 'OpenAI',
    tagline: 'GPT-4o and family',
    base_url: 'https://api.openai.com/v1',
    default_model: 'gpt-4o-mini',
    models: ['gpt-4o-mini', 'gpt-4o', 'o4-mini', 'o3'],
    key_label: 'OpenAI API key',
    key_url: 'https://platform.openai.com/api-keys',
  },
  {
    id: 'anthropic',
    name: 'Anthropic',
    tagline: 'Claude via OpenAI-compat',
    base_url: 'https://api.anthropic.com/v1',
    default_model: 'claude-sonnet-4-20250514',
    models: [
      'claude-sonnet-4-20250514',
      'claude-opus-4-20250514',
      'claude-haiku-4-20250514',
    ],
    key_label: 'Anthropic API key',
    key_url: 'https://console.anthropic.com/settings/keys',
  },
  {
    id: 'groq',
    name: 'Groq',
    tagline: 'Fast open models',
    base_url: 'https://api.groq.com/openai/v1',
    default_model: 'llama-3.3-70b-versatile',
    models: [
      'llama-3.3-70b-versatile',
      'llama-3.1-8b-instant',
      'mixtral-8x7b-32768',
    ],
    key_label: 'Groq API key',
    key_url: 'https://console.groq.com/keys',
  },
  {
    id: 'mistral',
    name: 'Mistral',
    tagline: 'Mistral Large and codestral',
    base_url: 'https://api.mistral.ai/v1',
    default_model: 'mistral-large-latest',
    models: ['mistral-large-latest', 'mistral-small-latest', 'codestral-latest'],
    key_label: 'Mistral key',
    key_url: 'https://console.mistral.ai/api-keys',
  },
  {
    id: 'google',
    name: 'Google Gemini',
    tagline: 'Gemini via OpenAI-compat',
    base_url: 'https://generativelanguage.googleapis.com/v1beta/openai',
    default_model: 'gemini-2.0-flash',
    models: ['gemini-2.0-flash', 'gemini-2.0-flash-lite', 'gemini-2.5-pro'],
    key_label: 'Google AI Studio key',
    key_url: 'https://aistudio.google.com/apikey',
  },
  {
    id: 'ollama',
    name: 'Ollama',
    tagline: 'Local models on this host',
    base_url: 'http://localhost:11434/v1',
    default_model: 'llama3.2',
    models: ['llama3.2', 'llama3.1', 'mistral', 'qwen2.5', 'codellama'],
    key_label: 'none (local)',
    key_url: 'https://ollama.com',
    local: true,
  },
  {
    id: 'vllm',
    name: 'vLLM',
    tagline: 'Local OpenAI-compat server',
    base_url: 'http://localhost:8000/v1',
    default_model: 'meta-llama/Meta-Llama-3-8B-Instruct',
    models: [
      'meta-llama/Meta-Llama-3-8B-Instruct',
      'meta-llama/Meta-Llama-3-70B-Instruct',
    ],
    key_label: 'none (local)',
    key_url: 'https://docs.vllm.ai',
    local: true,
  },
  {
    id: 'together',
    name: 'Together AI',
    tagline: 'Open models hosted',
    base_url: 'https://api.together.xyz/v1',
    default_model: 'meta-llama/Llama-3.3-70B-Instruct-Turbo',
    models: [
      'meta-llama/Llama-3.3-70B-Instruct-Turbo',
      'Qwen/Qwen2.5-72B-Instruct-Turbo',
      'deepseek-ai/DeepSeek-R1',
    ],
    key_label: 'Together key',
    key_url: 'https://api.together.xyz/settings/api-keys',
  },
  {
    id: 'deepseek',
    name: 'DeepSeek',
    tagline: 'DeepSeek chat and reasoner',
    base_url: 'https://api.deepseek.com/v1',
    default_model: 'deepseek-chat',
    models: ['deepseek-chat', 'deepseek-reasoner'],
    key_label: 'DeepSeek key',
    key_url: 'https://platform.deepseek.com/api_keys',
  },
]

/** Match a configured provider to a catalog preset (by base URL host or name). */
export function matchProviderPreset(
  provider: { name: string; base_url: string },
): ProviderPreset | undefined {
  const host = (() => {
    try {
      return new URL(provider.base_url).host.toLowerCase()
    } catch {
      return ''
    }
  })()
  const name = provider.name.toLowerCase()
  return (
    PROVIDER_PRESETS.find((p) => {
      try {
        return new URL(p.base_url).host.toLowerCase() === host
      } catch {
        return false
      }
    }) ||
    PROVIDER_PRESETS.find(
      (p) =>
        name.includes(p.name.toLowerCase()) ||
        p.name.toLowerCase().includes(name) ||
        name.includes(p.id),
    )
  )
}

/** Short stable alias from an upstream model id (e.g. meta-llama/Foo-Bar → foo-bar). */
export function shortModelAlias(upstream: string): string {
  const last = upstream.split('/').pop() || upstream
  return last
    .toLowerCase()
    .replace(/_\d{8,}$/g, '') // strip date suffixes like _20250514
    .replace(/-\d{8,}$/g, '')
    .replace(/[^a-z0-9._-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
    .slice(0, 48)
}

const ROLE_ALIASES = ['chat', 'fast', 'smart', 'code', 'cheap', 'reason'] as const

/** Suggested route aliases for the Add model route dialog. */
export function suggestRouteAliases(opts: {
  providerName: string
  upstreamModel: string
  existingAliases: string[]
}): string[] {
  const used = new Set(opts.existingAliases.map((a) => a.toLowerCase()))
  const out: string[] = []
  const add = (raw: string) => {
    const t = raw
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '')
    if (!t || used.has(t) || out.includes(t)) return
    out.push(t)
  }

  for (const role of ROLE_ALIASES) add(role)

  const short = shortModelAlias(opts.upstreamModel)
  if (short) {
    add(short)
    // Family nicknames
    if (/gpt-4o|gpt-5|o[1-9]/i.test(opts.upstreamModel)) add('openai')
    if (/claude/i.test(opts.upstreamModel)) add('claude')
    if (/gemini/i.test(opts.upstreamModel)) add('gemini')
    if (/mistral|codestral/i.test(opts.upstreamModel)) add('mistral')
    if (/llama/i.test(opts.upstreamModel)) add('llama')
    if (/deepseek/i.test(opts.upstreamModel)) add('deepseek')
    if (/qwen/i.test(opts.upstreamModel)) add('qwen')
  }

  const prov = opts.providerName
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
    .slice(0, 24)
  if (prov) {
    add(`${prov}-chat`)
    add(prov)
  }

  return out.slice(0, 10)
}


export const CATEGORY_LABELS: Record<ServerCategory, string> = {
  search: 'Search',
  docs: 'Docs & data',
  data: 'Docs & data',
  dev: 'Dev',
  productivity: 'Productivity',
  browser: 'Browser',
  local: 'Local',
}

/** Display order for Quick-add section headers. */
export const CATEGORY_ORDER: ServerCategory[] = [
  'search',
  'docs',
  'data',
  'dev',
  'productivity',
  'browser',
  'local',
]

/** Verified remote presets probed live 2026-09-07 (initialize → 200 SSE or expected auth error). */
export const SERVER_PRESETS: ServerPreset[] = [
  // —— Zero-config HTTP (click → connects) ——
  {
    id: 'deepwiki',
    name: 'DeepWiki',
    tagline: 'Explain any GitHub repo',
    category: 'docs',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.deepwiki.com/mcp',
    auth: { type: 'none' },
  },
  {
    id: 'exa',
    name: 'Exa Search',
    tagline: 'Neural web search for agents',
    category: 'search',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.exa.ai/mcp',
    auth: { type: 'none' },
  },
  {
    id: 'zapier',
    name: 'Zapier',
    tagline: 'Automate 8,000+ apps',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.zapier.com/mcp',
    auth: { type: 'oauth_device' },
  },

  // —— HTTP + API key (server up; needs token) ——
  {
    id: 'cloudflare',
    name: 'Cloudflare',
    tagline: 'Workers, DNS, and CDN tools',
    category: 'dev',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.cloudflare.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer CLOUDFLARE_API_TOKEN',
      key_label: 'Cloudflare API token',
      key_url: 'https://developers.cloudflare.com/fundamentals/api/',
    },
  },
  {
    id: 'stripe',
    name: 'Stripe',
    tagline: 'Payments and billing tools',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.stripe.com',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer STRIPE_SECRET_KEY',
      key_label: 'Stripe secret key',
      key_url: 'https://docs.stripe.com/mcp',
    },
  },
  {
    id: 'notion',
    name: 'Notion',
    tagline: 'Pages, databases, and search',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.notion.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer NOTION_TOKEN',
      key_label: 'Notion integration token',
      key_url: 'https://www.notion.so/my-integrations',
    },
  },
  {
    id: 'sentry',
    name: 'Sentry',
    tagline: 'Errors and performance issues',
    category: 'dev',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.sentry.dev/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer SENTRY_TOKEN',
      key_label: 'Sentry org token',
      key_url: 'https://docs.sentry.io/product/mcp/',
    },
  },
  {
    id: 'linear',
    name: 'Linear',
    tagline: 'Issues and project tracking',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.linear.app/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer LINEAR_API_KEY',
      key_label: 'Linear API key',
      key_url: 'https://linear.app/settings/api',
    },
  },
  {
    id: 'slack',
    name: 'Slack',
    tagline: 'Channels, messages, and search',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.slack.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer xoxb-…',
      key_label: 'Slack bot token',
      key_url: 'https://api.slack.com/apps',
    },
  },
  {
    id: 'github-copilot',
    name: 'GitHub (Copilot)',
    tagline: 'Repos, issues, and PRs',
    category: 'dev',
    transport: 'http',
    verified: true,
    base_url: 'https://api.githubcopilot.com/mcp/',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer ghp_…',
      key_label: 'GitHub personal access token',
      key_url: 'https://github.com/settings/tokens',
    },
  },
  {
    id: 'mongodb',
    name: 'MongoDB',
    tagline: 'Atlas and database tools',
    category: 'data',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.mongodb.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer MONGODB_API_KEY',
      key_label: 'MongoDB Atlas API key',
      key_url: 'https://www.mongodb.com/atlas/security',
    },
  },
  {
    id: 'clickup',
    name: 'ClickUp',
    tagline: 'Tasks and docs',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.clickup.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer CLICKUP_TOKEN',
      key_label: 'ClickUp personal token',
      key_url: 'https://clickup.com/me/settings/personal-access-tokens',
    },
  },
  {
    id: 'posthog',
    name: 'PostHog',
    tagline: 'Product analytics',
    category: 'data',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.posthog.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer POSTHOG_API_TOKEN',
      key_label: 'PostHog API token',
      key_url: 'https://posthog.com/settings/tokens',
    },
  },
  {
    id: 'airtable',
    name: 'Airtable',
    tagline: 'Bases and records',
    category: 'data',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.airtable.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer AIRTABLE_TOKEN',
      key_label: 'Airtable personal access token',
      key_url: 'https://airtable.com/create/tokens',
    },
  },
  {
    id: 'intercom',
    name: 'Intercom',
    tagline: 'Customer conversations',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.intercom.com/mcp',
    auth: {
      type: 'static',
      header: 'Authorization',
      value_hint: 'Bearer INTERCOM_TOKEN',
      key_label: 'Intercom API token',
      key_url:
        'https://app.intercom.com/admin/your_team_id/settings/api_integration',
    },
  },
  {
    id: 'higgsfield',
    name: 'Higgsfield',
    tagline: 'AI image and video generation',
    category: 'productivity',
    transport: 'http',
    verified: true,
    base_url: 'https://mcp.higgsfield.ai/mcp',
    auth: { type: 'oauth_device' },
  },

  // —— Local stdio (official / documented command lines) ——
  {
    id: 'fs',
    name: 'Filesystem',
    tagline: 'Read and write local files',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-filesystem', '~/Desktop'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'git',
    name: 'Git',
    tagline: 'Inspect local git history',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-git'],
    env: [{ key: 'GIT_REPO_PATH', value_hint: 'path to a local repo (optional)' }],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'github-local',
    name: 'GitHub (local)',
    tagline: 'GitHub API via local process',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-github'],
    env: [{ key: 'GITHUB_PERSONAL_ACCESS_TOKEN', value_hint: 'ghp_…' }],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'memory',
    name: 'Memory',
    tagline: 'Persistent knowledge graph',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-memory'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'time',
    name: 'Time',
    tagline: 'Clock and timezone helpers',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-time'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'fetch',
    name: 'Fetch',
    tagline: 'HTTP fetch for agents (Python)',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'uvx',
    args: ['mcp-server-fetch'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'everything',
    name: 'Everything (demo)',
    tagline: 'Official test server — does it work?',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-everything'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'playwright',
    name: 'Playwright',
    tagline: 'Browser automation',
    category: 'browser',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@playwright/mcp@latest'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'puppeteer',
    name: 'Puppeteer',
    tagline: 'Headless Chrome tooling',
    category: 'browser',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-puppeteer'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'amap',
    name: 'AMap',
    tagline: 'Maps and geocoding (China)',
    category: 'search',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@amap/amap-maps-mcp-server'],
    env: [{ key: 'AMAP_MAPS_API_KEY', value_hint: 'AMap key' }],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'sequential-thinking',
    name: 'Sequential Thinking',
    tagline: 'Step-by-step reasoning tools',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-sequential-thinking'],
    auth: { type: 'none' },
    needs_local: true,
  },
  {
    id: 'sqlite',
    name: 'SQLite',
    tagline: 'Query a local SQLite database',
    category: 'local',
    transport: 'stdio',
    verified: true,
    command: 'uvx',
    args: ['mcp-server-sqlite'],
    env: [{ key: 'DATABASE_PATH', value_hint: 'path to .sqlite/.db' }],
    auth: { type: 'none' },
    needs_local: true,
  },
]

/** Curated first-run row: zero-config remotes + locals + one popular key-required. */
export const START_HERE_IDS = [
  'deepwiki',
  'exa',
  'time',
  'everything',
  'notion',
] as const

export function startHerePresets(): ServerPreset[] {
  return START_HERE_IDS.map((id) => {
    const p = SERVER_PRESETS.find((s) => s.id === id)
    if (!p) throw new Error(`start-here preset missing: ${id}`)
    return p
  })
}

/** Zero-config (or OAuth) presets may show "Add now" and auto-submit. */
export function canAutoAdd(preset: ServerPreset): boolean {
  if (preset.auth.type === 'static') return false
  if (preset.env && preset.env.some((e) => !e.value_hint.includes('optional'))) {
    // Required env (no "optional" in hint) → land on Advanced to fill values.
    const required = preset.env.filter((e) => !/optional/i.test(e.value_hint))
    if (required.length > 0) return false
  }
  return preset.auth.type === 'none' || preset.auth.type === 'oauth_device'
}

export function findServerPreset(id: string): ServerPreset | undefined {
  return SERVER_PRESETS.find((p) => p.id === id)
}

export function findProviderPreset(id: string): ProviderPreset | undefined {
  return PROVIDER_PRESETS.find((p) => p.id === id)
}

export function presetsByCategory(): { category: ServerCategory; label: string; presets: ServerPreset[] }[] {
  const seen = new Set<string>()
  const groups: { category: ServerCategory; label: string; presets: ServerPreset[] }[] = []
  for (const cat of CATEGORY_ORDER) {
    const label = CATEGORY_LABELS[cat]
    if (seen.has(label)) {
      // Merge docs+data under one header already emitted.
      const existing = groups.find((g) => g.label === label)
      if (existing) {
        existing.presets.push(...SERVER_PRESETS.filter((p) => p.category === cat))
      }
      continue
    }
    seen.add(label)
    const presets = SERVER_PRESETS.filter((p) => p.category === cat)
    if (presets.length) groups.push({ category: cat, label, presets })
  }
  return groups
}
