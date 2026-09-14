# Upstream authentication

How the gateway authenticates to **remote MCP servers** (not how clients authenticate to the gateway).

## Modes

| `auth_type` | Behavior |
|-------------|----------|
| `none` | No upstream `Authorization` header. |
| `static` | Inject `auth_header` + `auth_value` (Phase 1–8 behavior). Empty value ≡ none. |
| `oauth_device` | Device-code flow (RFC 8628 + vendor quirks). Primary path for headless gateways. |
| `oauth_pkce` | Authorization-code + PKCE via browser redirect to `/oauth/redirect`. Best-effort. |

Flat create-server fields (`auth_header` / `auth_value`) still work and map to `static`. Prefer:

```json
{ "name": "…", "base_url": "…", "auth": { "type": "oauth_device" } }
```

## Security notes (v1)

- OAuth tokens are stored **per account** (one connection shared by that account’s gateway keys).
- Tokens are **plaintext in SQLite** in v1 — same documented status as `auth_value`. Encryption-at-rest is v1.1.
- Refresh tokens are persisted; `TokenGuard` refreshes when `expires_at` is within 60s. Refresh failure → status `expired`, proxy returns **401** with `upstream oauth expired — reconnect` (not 500).

All upstream OAuth logic lives in `pkg/auth/upstream`. Indexer and proxy only call `TokenGuard` — never invent ad-hoc token logic there.

## Higgsfield walkthrough (canonical device flow)

Verified 2026-09-06 against `https://mcp.higgsfield.ai/mcp`.

1. MCP returns `401` with
   `WWW-Authenticate: Bearer resource_metadata="https://mcp.higgsfield.ai/.well-known/oauth-protected-resource/mcp", scope="openid email offline_access"`.
2. Protected-resource metadata advertises two authorization servers; for headless use pick the **device** AS:
   `https://fnf-device-auth.higgsfield.ai`.
3. Device endpoints (non-RFC paths — see `pkg/auth/upstream/hints.go`):

```text
POST https://fnf-device-auth.higgsfield.ai/authorize
Content-Type: application/json
{"client_id":"tokencontrolplane","scope":"openid email offline_access"}

→ {"device_code":"…","verification_uri":"https://higgsfield.ai/device?code=…","expires_in":900,"interval":3}

POST https://fnf-device-auth.higgsfield.ai/token
{"grant_type":"urn:ietf:params:oauth:grant-type:device_code","device_code":"…","client_id":"tokencontrolplane"}

pending:  {"detail":"authorization_pending"}   (HTTP 200)
granted:  standard token JSON
expired:  {"detail":"expired_token"}
```

CLI reference: `scripts/higgsfield-connect.sh`. Dashboard: add server with OAuth sign-in → Connect → open verification URL → status flips to **connected** → tools index.

## Fallback table rule

When onboarding a new OAuth MCP server with non-standard paths, **add its quirks to** `pkg/auth/upstream/hints.go` (`deviceEndpointHints`). Do not special-case hosts in the proxy or indexer.

Discovery order for device endpoints:

1. Authorization-server metadata (`device_authorization_endpoint` / `token_endpoint`)
2. Protected-resource `higgsfield_auth_hints` (or equivalent) picking `flow == device_code`
3. Host fallback table in `hints.go`
4. RFC 8628 defaults (`/device_authorization`, `/token`)

Pending/error shapes: parse both RFC `{"error":"authorization_pending"}` and Higgsfield `{"detail":"authorization_pending"}`.

## API surface

| Method | Path | Notes |
|--------|------|-------|
| `POST` | `/api/v1/servers/{id}/connect` | Begin flow; device starts background poll |
| `GET` | `/api/v1/servers/{id}/connect` | `pending` / `connected` / `expired` / `disconnected` |
| `POST` | `/api/v1/servers/{id}/reconnect` | Delete token, start fresh |
| `DELETE` | `/api/v1/servers/{id}/oauth` | Disconnect |
| `GET` | `/oauth/redirect` | Public PKCE callback (no JWT) |
