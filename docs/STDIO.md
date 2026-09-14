# Stdio transport (local MCP servers)

Phase 11b — the gateway can spawn local MCP servers (`npx`, `uvx`, absolute binaries) and expose them on the same `/mcp/{server_id}` streamable-HTTP URL clients already use for remote servers.

## How it works

1. Dashboard create: **Local (stdio)** with `command`, `args[]`, `env{}`, optional `workdir`.
2. Gateway stores the row (`transport=stdio`, empty `base_url`, `auth_type=none`). Secrets live in `env` (e.g. `OPENAI_API_KEY`), never in OAuth columns.
3. First request (or index) lazy-starts a subprocess via mcp-go's stdio client.
4. Inbound JSON-RPC is decoded and dispatched to the child; responses are plain `application/json`. If the client `Accept`s only `text/event-stream`, the gateway wraps one `data:` frame (deliberate simplification — we generate responses, we do not forward upstream SSE).

Paste-to-convert: paste a Cursor/Claude `mcp.json` into the add-server dialog; `POST /api/v1/servers/parse-mcp-json` extracts entries.

## PATH / env / cwd

| Concern | Behavior |
|---------|----------|
| PATH | Bare command names resolved with `exec.LookPath` at spawn. Missing → status `error` with exact message: `command 'npx' not found in PATH on the gateway host`. |
| Env | `env` JSON object is **merged over** the gateway process environment (never replaces it). |
| Cwd | If `cwd_isolation` (default on) and `workdir` empty → `~/.tokencontrolplane/sandbox/<server_id>/` (0700). |
| Spawn | `exec.Command(command, args...)` — never a shell string. |

Warn-only PATH check at submit: `POST /api/v1/servers/check-command`.

## In-flight cap

Max **4** concurrent `tools/call`s per stdio server. Overflow waits 30s then returns JSON-RPC `-32000 "server busy"`. Prevents one slow generation from wedging the process.

## Crash recovery

Spawn death / write error / initialize timeout (10s) → kill + restart. After **3 strikes** the circuit opens (shared breaker). Manual **Restart & reindex** clears strikes and respawns. Activity event: `server_restart` (throttled).

## Stderr

Child stderr is drained into a bounded buffer. Status API returns the last ~20 sanitized lines — never a raw unbounded stream.

## Tool policy

Same `pkg/policy` filter as HTTP: disabled tools hidden; custom key allowlists apply to `tools/list` and `tools/call` on the bridge.

## Honest limits (v1)

- **No Windows stdio spawn** — Linux/macOS only; HTTP path works everywhere.
- One gateway-owned process per stdio server (not for hundreds of children).
- No cgroup CPU/mem limits — the in-flight cap is the v1 backpressure.
- No OAuth for stdio (env-only auth).
- No marketplace / auto-discovery UI beyond paste-to-convert.
