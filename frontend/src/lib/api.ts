const TOKEN_KEY = 'tcp_token'
const BASE = import.meta.env.VITE_API_BASE || ''

export type Plan = 'free' | 'pro' | 'team'

export interface User {
  id: string
  email: string
  name: string
  role: string
  account_id: string
  plan: Plan
  max_servers: number
  max_seats: number
  seats_used?: number
  tool_policy?: boolean
  totp_enabled?: boolean
}

export interface AuthResponse {
  token: string
  user?: User
}

export interface MFARequiredResponse {
  mfa_required: true
  mfa_token: string
}

export type LoginResult = AuthResponse | MFARequiredResponse

export function isMFARequired(res: LoginResult): res is MFARequiredResponse {
  return 'mfa_required' in res && res.mfa_required === true
}

export interface TwoFAStatus {
  enabled: boolean
  confirmed_at?: string | null
  recovery_remaining: number
}

export interface TwoFASetup {
  secret: string
  provisioning_uri: string
  qr: string
}

export interface TwoFAConfirm {
  recovery_codes: string[]
}

/** 429 from MFA (or other rate-limited public auth) with Retry-After. */
export class RateLimitError extends Error {
  retryAfter: number
  constructor(message: string, retryAfter: number) {
    super(message)
    this.name = 'RateLimitError'
    this.retryAfter = retryAfter
  }
}

export interface DailyUsage {
  date: string
  tokens: number
}

export interface UsageSummary {
  period: string
  tokens_used: number
  monthly_budget: number
  requests_today: number
  daily: DailyUsage[]
  servers_count?: number
  active_keys?: number
  llm?: {
    tokens: number
    tokens_exact: number
    tokens_estimated: number
    providers: number
  }
}

export interface ToolUsage {
  name: string
  server_name?: string
  call_count: number
}

export interface IndexedTool {
  name: string
  description?: string
}

export interface MCPServer {
  id: string
  name: string
  base_url: string
  enabled: boolean
  tool_count: number
  auth_header?: string
  auth_type?: 'none' | 'static' | 'oauth_device' | 'oauth_pkce' | string
  oauth_status?: 'disconnected' | 'pending' | 'connected' | 'expired' | string
  transport?: 'http' | 'stdio' | string
  command?: string
  process?: { state?: string; in_flight?: number; strikes?: number }
  created_at?: string
  last_index_error?: string
  last_latency_ms?: number | null
}

export interface CreateServerInput {
  name: string
  base_url?: string
  auth_header?: string
  auth_value?: string
  auth_type?: string
  auth?: {
    type: 'none' | 'static' | 'oauth_device' | 'oauth_pkce'
    header?: string
    value?: string
  }
  transport?: 'http' | 'stdio'
  command?: string
  args?: string[]
  env?: Record<string, string>
  workdir?: string
  cwd_isolation?: boolean
}

export interface CreateServerResponse extends MCPServer {
  tools: IndexedTool[]
  indexing_error: string | null
}

export interface PatchServerInput {
  name?: string
  enabled?: boolean
  base_url?: string
  auth_header?: string
  auth_value?: string
  auth_type?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  workdir?: string
  cwd_isolation?: boolean
}

export interface OAuthConnectSession {
  flow: 'device' | 'pkce' | string
  device_code?: string
  verification_uri?: string
  verification_code?: string
  authorize_url?: string
  expires_in: number
  interval?: number
}

export interface OAuthConnectStatus {
  status: 'disconnected' | 'pending' | 'connected' | 'expired' | string
  flow?: string
  verification_uri?: string
  verification_code?: string
  authorize_url?: string
  expires_in?: number
  interval?: number
}

export interface StdioProcessStatus {
  transport?: string
  state?: string
  pid?: number
  uptime_sec?: number
  in_flight?: number
  strikes?: number
  spawn_count?: number
  last_error?: string
  last_exit_code?: number
  stderr_tail?: string[]
}

export interface CircuitStatus {
  state: 'closed' | 'open' | 'half_open' | string
  consecutive_failures?: number
  last_failure_at?: string | null
  transport?: string
  process?: StdioProcessStatus
}

export interface PasteMCPEntry {
  name: string
  transport: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  base_url?: string
}

export interface APIKey {
  id: string
  name: string
  prefix?: string
  killed: boolean
  last_used_at?: string | null
  tokens_used: number
  monthly_budget: number
  created_at?: string
  owner?: { id: string; name: string } | null
  tool_policy?: {
    mode: 'all' | 'custom' | string
    allowed: string[]
    count?: number
  }
  server_policy?: {
    mode: 'all' | 'custom' | string
    allowed: string[]
    count?: number
  }
  provider_policy?: {
    mode: 'all' | 'custom' | string
    allowed: string[]
    count?: number
  }
}

export interface TeamUser {
  id: string
  email: string
  name: string
  role: 'admin' | 'member' | string
  created_at: string
  totp_enabled?: boolean
}

export interface TeamUsersResponse {
  users: TeamUser[]
  seats_used: number
  seats_max: number
}

export interface PendingInvite {
  id: string
  created_by_name: string
  role: string
  expires_at: string
  created_at: string
}

export interface CreateInviteResponse {
  code: string
  path: string
  url?: string
  expires_at: string
}

export interface EmailInviteResponse {
  invite: CreateInviteResponse
  email_sent: boolean
  email_error?: string
}

export interface SMTPConfig {
  enabled: boolean
  host: string
  port: number
  username: string
  from: string
  from_name: string
  tls_mode: 'starttls' | 'tls' | 'plain' | string
  has_password: boolean
}

export interface EmailTemplates {
  invite: string
  reset: string
}

export interface PublicInvite {
  account_name: string
  role: string
}

export interface CatalogTool {
  name: string
  description?: string
  input_schema?: unknown
  enabled: boolean
  calls_30d: number
}

export interface KeyToolAccess {
  mode: 'all' | 'custom' | string
  allowed: string[]
}

export interface KeyServerAllowed {
  id: string
  name: string
  monthly_budget: number
  tokens_used: number
}

export interface KeyServerAccess {
  mode: 'all' | 'custom' | string
  allowed: KeyServerAllowed[]
}

export interface KeyServerUsage {
  server_id: string
  name: string
  tokens: number
  requests: number
  monthly_budget: number
}

export interface LLMProvider {
  id: string
  name: string
  base_url: string
  enabled: boolean
  auth_header?: string
  default_model?: string
  timeout_seconds?: number
  model_count?: number
  last_health_error?: string
  created_at?: string
}

export interface LLMModel {
  id: string
  name: string
  model: string
  provider_id: string
  provider_name?: string
  fallback_model_id?: string | null
  fallback_name?: string
  created_at?: string
}

export interface KeyProviderAllowed {
  id: string
  name: string
  monthly_budget: number
  tokens_used: number
}

export interface KeyProviderAccess {
  mode: 'all' | 'custom' | string
  allowed: KeyProviderAllowed[]
}

export interface KeyProviderUsage {
  provider_id: string
  name: string
  tokens: number
  tokens_exact?: number
  tokens_estimated?: number
  requests: number
  monthly_budget: number
}

export interface ClientSnippets {
  claude_desktop: string
  cursor: string
  windsurf: string
}

export interface CreateKeyResponse {
  id: string
  name: string
  key: string
  snippets: ClientSnippets
}

export interface BillingSession {
  url: string
}

/** Caps block from GET /api/v1/pricing (mirrors license.CapsForPlan). */
export interface PricingCaps {
  max_servers: number
  max_seats: number
  max_keys: number
  monthly_tokens: number
  tool_policy: boolean
}

export interface PricingPlan {
  key: 'pro' | 'team'
  label: string
  price: number
  founders_price: number
  window_months: number
  founders_window: number
  caps: PricingCaps
}

export interface PricingResponse {
  plans: PricingPlan[]
  founders_limit: number
  founders_remaining: number
  founders_available: boolean
  founders_deadline: string | null
  ever_works_forever: boolean
  renewal_note?: string
}

/** Thin fallback if /pricing fetch fails — numbers must match pkg/billing/pricing.go. */
export const PRICING_FALLBACK: PricingResponse = {
  plans: [
    {
      key: 'pro',
      label: 'Pro',
      price: 249,
      founders_price: 179,
      window_months: 12,
      founders_window: 24,
      caps: {
        max_servers: 10,
        max_seats: 10,
        max_keys: 20,
        monthly_tokens: 0,
        tool_policy: true,
      },
    },
    {
      key: 'team',
      label: 'Team',
      price: 599,
      founders_price: 449,
      window_months: 12,
      founders_window: 24,
      caps: {
        max_servers: 25,
        max_seats: 25,
        max_keys: 50,
        monthly_tokens: 0,
        tool_policy: true,
      },
    },
  ],
  founders_limit: 100,
  founders_remaining: 100,
  founders_available: true,
  founders_deadline: null,
  ever_works_forever: true,
}

export interface LicenseInfo {
  id: string
  plan: Plan
  key: string
  purchased_at: string
  expires_at: string
  revoked: boolean
  needs_manual_key: boolean
  expired: boolean
  window_months?: number
  is_founders?: boolean
  days_to_expiry?: number
  ever_works_forever?: boolean
}

export interface OnboardingState {
  has_servers: boolean
  has_keys: boolean
  onboarded: boolean
}

export interface ActivityEvent {
  id: string
  kind: string
  subject: string
  summary: string
  detail: string
  created_at: string
}

export interface UpdateCheck {
  current: string
  latest: string
  available: boolean
  checked_at: string
}

export interface UpdateWindow {
  expires_at: string
  active: boolean
  is_founders: boolean
  days_left: number
}

export interface VersionInfo {
  version: string
  commit: string
  built: string
  schema: number
  latest: string
  update_available: boolean
  update_window: UpdateWindow | null
}

export interface ReleaseNotes {
  tag: string
  published_at: string | null
  body_markdown: string
}

export interface ApplyUpdateResult {
  status: string
  restart: 'systemd' | 'manual' | string
  message: string
  version_before?: string
  schema?: number
  schema_note?: string
}

function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

export function isAuthenticated(): boolean {
  return !!getToken()
}

type RequestOptions = RequestInit & {
  /** Public auth calls: surface 401 JSON error verbatim; do not clear token / redirect. */
  skipAuthRedirect?: boolean
}

async function parseErrorMessage(res: Response): Promise<string> {
  let message = res.statusText
  try {
    const body = await res.json()
    message = body.error || body.message || message
  } catch {
    /* ignore */
  }
  return message
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { skipAuthRedirect, ...init } = options
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const res = await fetch(`${BASE}${path}`, { ...init, headers })

  if (res.status === 401) {
    if (skipAuthRedirect) {
      throw new Error(await parseErrorMessage(res))
    }
    clearToken()
    if (window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
    throw new Error('Unauthorized')
  }

  if (!res.ok) {
    const message = await parseErrorMessage(res)
    if (res.status === 429) {
      const ra = res.headers.get('Retry-After')
      const retryAfter = ra ? parseInt(ra, 10) : 60
      throw new RateLimitError(message, Number.isFinite(retryAfter) ? retryAfter : 60)
    }
    throw new Error(message)
  }

  if (res.status === 204) {
    return undefined as T
  }

  return res.json() as Promise<T>
}

/** Public auth endpoints — never clear session or redirect on 401. */
function requestPublic<T>(path: string, options: RequestInit = {}): Promise<T> {
  return request<T>(path, { ...options, skipAuthRedirect: true })
}

export const api = {
  publicConfig() {
    return request<{ registration_enabled: boolean }>('/api/v1/public-config')
  },

  register(email: string, password: string, accountName: string) {
    return requestPublic<AuthResponse>('/api/v1/register', {
      method: 'POST',
      body: JSON.stringify({
        email,
        password,
        account_name: accountName,
      }),
    })
  },

  login(email: string, password: string) {
    return requestPublic<LoginResult>('/api/v1/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
  },

  loginMFA(mfaToken: string, code: string) {
    return requestPublic<AuthResponse>('/api/v1/login/mfa', {
      method: 'POST',
      body: JSON.stringify({ mfa_token: mfaToken, code }),
    })
  },

  get2FA() {
    return request<TwoFAStatus>('/api/v1/2fa')
  },

  setup2FA() {
    return request<TwoFASetup>('/api/v1/2fa/setup', { method: 'POST', body: '{}' })
  },

  confirm2FA(code: string) {
    return request<TwoFAConfirm>('/api/v1/2fa/confirm', {
      method: 'POST',
      body: JSON.stringify({ code }),
    })
  },

  delete2FA() {
    return request<void>('/api/v1/2fa', { method: 'DELETE' })
  },

  me() {
    return request<User>('/api/v1/me')
  },

  usage() {
    return request<UsageSummary>('/api/v1/usage')
  },

  usageTools(serverId?: string) {
    const q = serverId ? `?server_id=${encodeURIComponent(serverId)}` : ''
    return request<ToolUsage[]>(`/api/v1/usage/tools${q}`)
  },

  onboarding() {
    return request<OnboardingState>('/api/v1/onboarding')
  },

  completeOnboarding() {
    return request<{ onboarded: boolean }>('/api/v1/onboarding/complete', {
      method: 'POST',
    })
  },

  activity(limit = 50) {
    return request<ActivityEvent[]>(`/api/v1/activity?limit=${limit}`)
  },

  updateCheck() {
    return request<UpdateCheck>('/api/v1/update-check')
  },

  version() {
    return request<VersionInfo>('/api/v1/version')
  },

  releaseNotes(tag: string) {
    const t = tag.startsWith('v') ? tag : `v${tag}`
    return request<ReleaseNotes>(`/api/v1/releases/${encodeURIComponent(t)}`)
  },

  applyUpdate(body?: { file?: string; sha256?: string }) {
    return request<ApplyUpdateResult>('/api/v1/update', {
      method: 'POST',
      body: JSON.stringify(body ?? {}),
    })
  },

  listServers() {
    return request<MCPServer[]>('/api/v1/servers')
  },

  createServer(input: CreateServerInput) {
    return request<CreateServerResponse>('/api/v1/servers', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  checkCommand(command: string) {
    return request<{ found: boolean; resolved?: string; error?: string }>(
      '/api/v1/servers/check-command',
      { method: 'POST', body: JSON.stringify({ command }) },
    )
  },

  parseMCPJSON(raw: string) {
    return request<{ servers: PasteMCPEntry[] }>('/api/v1/servers/parse-mcp-json', {
      method: 'POST',
      body: JSON.stringify({ raw }),
    })
  },

  patchServer(id: string, input: PatchServerInput) {
    return request<MCPServer>(`/api/v1/servers/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  },

  deleteServer(id: string) {
    return request<void>(`/api/v1/servers/${id}`, { method: 'DELETE' })
  },

  reindexServer(id: string) {
    return request<CreateServerResponse>(`/api/v1/servers/${id}/reindex`, {
      method: 'POST',
    })
  },

  connectServer(id: string, flow?: 'device' | 'pkce') {
    return request<OAuthConnectSession>(`/api/v1/servers/${id}/connect`, {
      method: 'POST',
      body: JSON.stringify(flow ? { flow } : {}),
    })
  },

  connectStatus(id: string) {
    return request<OAuthConnectStatus>(`/api/v1/servers/${id}/connect`)
  },

  reconnectServer(id: string, flow?: 'device' | 'pkce') {
    return request<OAuthConnectSession>(`/api/v1/servers/${id}/reconnect`, {
      method: 'POST',
      body: JSON.stringify(flow ? { flow } : {}),
    })
  },

  disconnectOAuth(id: string) {
    return request<void>(`/api/v1/servers/${id}/oauth`, { method: 'DELETE' })
  },

  listServerTools(id: string) {
    return request<CatalogTool[]>(`/api/v1/servers/${id}/tools`)
  },

  patchServerTool(id: string, name: string, enabled: boolean) {
    return request<{ name: string; enabled: boolean }>(`/api/v1/servers/${id}/tools`, {
      method: 'PATCH',
      body: JSON.stringify({ name, enabled }),
    })
  },

  bulkServerTools(
    id: string,
    enabled: boolean,
    scope: 'all' | 'selected',
    names?: string[],
  ) {
    return request<{ updated: number }>(`/api/v1/servers/${id}/tools/bulk`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled, scope, names }),
    })
  },

  getKeyTools(id: string) {
    return request<KeyToolAccess>(`/api/v1/keys/${id}/tools`)
  },

  putKeyTools(id: string, mode: 'all' | 'custom', allowed?: string[]) {
    return request<KeyToolAccess>(`/api/v1/keys/${id}/tools`, {
      method: 'PUT',
      body: JSON.stringify({ mode, allowed: allowed ?? [] }),
    })
  },

  getKeyServers(id: string) {
    return request<KeyServerAccess>(`/api/v1/keys/${id}/servers`)
  },

  putKeyServers(
    id: string,
    mode: 'all' | 'custom',
    allowed: string[],
    budgets: Record<string, number>,
  ) {
    return request<KeyServerAccess>(`/api/v1/keys/${id}/servers`, {
      method: 'PUT',
      body: JSON.stringify({ mode, allowed, budgets }),
    })
  },

  usageKeyServers(id: string) {
    return request<KeyServerUsage[]>(`/api/v1/usage/keys/${id}/servers`)
  },

  usageServers() {
    return request<
      {
        server_id: string
        name: string
        tokens: number
        requests: number
        by_key: { key_id: string; name: string; tokens: number; requests: number }[]
      }[]
    >('/api/v1/usage/servers')
  },

  listLLMProviders() {
    return request<LLMProvider[]>('/api/v1/llm/providers')
  },

  createLLMProvider(input: {
    name: string
    base_url: string
    auth_header?: string
    auth_value?: string
    default_model?: string
    timeout_seconds?: number
  }) {
    return request<LLMProvider>('/api/v1/llm/providers', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  patchLLMProvider(
    id: string,
    input: Partial<{
      name: string
      base_url: string
      auth_header: string
      auth_value: string
      default_model: string
      timeout_seconds: number
      enabled: boolean
    }>,
  ) {
    return request<LLMProvider>(`/api/v1/llm/providers/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  },

  deleteLLMProvider(id: string) {
    return request<void>(`/api/v1/llm/providers/${id}`, { method: 'DELETE' })
  },

  healthLLMProvider(id: string) {
    return request<{ ok: boolean; latency_ms: number; error: string }>(
      `/api/v1/llm/providers/${id}/health`,
    )
  },

  listLLMModels() {
    return request<LLMModel[]>('/api/v1/llm/models')
  },

  createLLMModel(input: {
    name: string
    model: string
    provider_id: string
    fallback_model_id?: string | null
  }) {
    return request<LLMModel>('/api/v1/llm/models', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  patchLLMModel(
    id: string,
    input: Partial<{
      name: string
      model: string
      provider_id: string
      fallback_model_id: string | null
    }>,
  ) {
    return request<LLMModel>(`/api/v1/llm/models/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    })
  },

  deleteLLMModel(id: string) {
    return request<void>(`/api/v1/llm/models/${id}`, { method: 'DELETE' })
  },

  getKeyProviders(id: string) {
    return request<KeyProviderAccess>(`/api/v1/keys/${id}/providers`)
  },

  putKeyProviders(
    id: string,
    mode: 'all' | 'custom',
    allowed: string[],
    budgets: Record<string, number>,
  ) {
    return request<KeyProviderAccess>(`/api/v1/keys/${id}/providers`, {
      method: 'PUT',
      body: JSON.stringify({ mode, allowed, budgets }),
    })
  },

  usageKeyProviders(id: string) {
    return request<KeyProviderUsage[]>(`/api/v1/usage/keys/${id}/providers`)
  },

  usageProviders() {
    return request<
      {
        provider_id: string
        name: string
        tokens: number
        tokens_exact: number
        tokens_estimated: number
        requests: number
      }[]
    >('/api/v1/usage/providers')
  },

  listKeys() {
    return request<APIKey[]>('/api/v1/keys')
  },

  createKey(name: string, serverId?: string) {
    return request<CreateKeyResponse>('/api/v1/keys', {
      method: 'POST',
      body: JSON.stringify({ name, server_id: serverId }),
    })
  },

  deleteKey(id: string) {
    return request<void>(`/api/v1/keys/${id}`, { method: 'DELETE' })
  },

  killKey(id: string) {
    return request<{ status: string }>(`/api/v1/keys/${id}/kill`, {
      method: 'POST',
    })
  },

  unkillKey(id: string) {
    return request<{ status: string }>(`/api/v1/keys/${id}/unkill`, {
      method: 'POST',
    })
  },

  billingCheckout(plan: Plan = 'pro') {
    return request<BillingSession>(`/api/v1/billing/checkout?plan=${plan}`, {
      method: 'POST',
    })
  },

  pricing() {
    return request<PricingResponse>('/api/v1/pricing')
  },

  /** Alias for checkout — one-time Lemon Squeezy purchase. */
  buy(plan: Plan = 'pro') {
    return this.billingCheckout(plan)
  },

  billingPortal() {
    return request<BillingSession>('/api/v1/billing/portal', {
      method: 'POST',
    })
  },

  myLicense() {
    return request<LicenseInfo>('/api/v1/licenses/me')
  },

  reissueLicense(email?: string) {
    return request<{ key: string }>('/api/v1/licenses/reissue', {
      method: 'POST',
      body: JSON.stringify(email ? { email } : {}),
    })
  },

  listUsers() {
    return request<TeamUsersResponse>('/api/v1/users')
  },

  patchUserRole(id: string, role: 'admin' | 'member') {
    return request<{ id: string; role: string }>(`/api/v1/users/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ role }),
    })
  },

  deleteUser(id: string) {
    return request<void>(`/api/v1/users/${id}`, { method: 'DELETE' })
  },

  listInvites() {
    return request<PendingInvite[]>('/api/v1/invites')
  },

  createInvite() {
    return request<CreateInviteResponse>('/api/v1/invites', {
      method: 'POST',
      body: '{}',
    })
  },

  createEmailInvite(email: string) {
    return request<EmailInviteResponse>('/api/v1/invites/email', {
      method: 'POST',
      body: JSON.stringify({ email }),
    })
  },

  listEmailInvites() {
    return request<
      { id: string; email: string; invite_id: string; sent_at: string; created_by: string }[]
    >('/api/v1/invites/email')
  },

  deleteInvite(id: string) {
    return request<void>(`/api/v1/invites/${id}`, { method: 'DELETE' })
  },

  publicInvite(code: string) {
    return request<PublicInvite>(`/api/v1/invites/public/${encodeURIComponent(code)}`)
  },

  join(code: string, email: string, password: string, name: string) {
    return requestPublic<AuthResponse>('/api/v1/join', {
      method: 'POST',
      body: JSON.stringify({ code, email, password, name }),
    })
  },

  getSMTP() {
    return request<SMTPConfig>('/api/v1/settings/smtp')
  },

  putSMTP(body: Partial<SMTPConfig> & { password?: string; enabled?: boolean }) {
    return request<SMTPConfig>('/api/v1/settings/smtp', {
      method: 'PUT',
      body: JSON.stringify(body),
    })
  },

  testSMTP(to: string) {
    return request<{ sent: boolean }>('/api/v1/settings/smtp/test', {
      method: 'POST',
      body: JSON.stringify({ to }),
    })
  },

  getTemplates() {
    return request<EmailTemplates>('/api/v1/settings/templates')
  },

  putTemplates(invite: string, reset: string) {
    return request<EmailTemplates>('/api/v1/settings/templates', {
      method: 'PUT',
      body: JSON.stringify({ invite, reset }),
    })
  },

  requestPasswordReset(email: string) {
    return requestPublic<{ ok: boolean }>('/api/v1/password-reset/request', {
      method: 'POST',
      body: JSON.stringify({ email }),
    })
  },

  confirmPasswordReset(token: string, newPassword: string, confirmPassword: string) {
    return requestPublic<{ ok: boolean }>('/api/v1/password-reset/confirm', {
      method: 'POST',
      body: JSON.stringify({
        token,
        new_password: newPassword,
        confirm_password: confirmPassword,
      }),
    })
  },

  changeMyPassword(current: string, newPassword: string, confirm: string) {
    return request<{ ok: boolean }>('/api/v1/me/password', {
      method: 'PUT',
      body: JSON.stringify({ current, new: newPassword, confirm }),
    })
  },

  adminResetPassword(userId: string, newPassword?: string) {
    return request<{ ok: boolean; email_sent: boolean; new_password?: string }>(
      `/api/v1/users/${userId}/reset-password`,
      {
        method: 'POST',
        body: JSON.stringify(newPassword ? { new_password: newPassword } : {}),
      },
    )
  },

  serverStatus(id: string) {
    return request<CircuitStatus>(`/api/v1/servers/${id}/status`)
  },
}
