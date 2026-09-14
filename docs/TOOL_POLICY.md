# Tool policy (Phase 11a)

Per-tool visibility, per-key **server** allowlists, and per-server budgets for Pro/Team.

## Precedence

1. **Server scope** — if `server_policy=custom` and the server is not granted → HTTP **403**  
   `{"error":"API key is not authorized for server 'X'"}` (no upstream).
2. **Per-server budget** — if a monthly budget B>0 is set for that server and  
   `ServerTokensUsed[server] >= B` → HTTP **402**  
   `{"error":"Per-server budget limit exceeded for 'X'. Access suspended by TokenControlPlane."}`  
   Other servers on the same key keep working (not a key kill).
3. **Tool scope** — disabled tools hidden from everyone; `tool_policy=custom` allowlists names.

Key-level monthly budget (existing middleware) still applies independently to the sum of all servers.

## Budgets (two ceilings)

A request is allowed only if the key is under **both**:

- its key-level `monthly_budget` (0 = unlimited), and
- its per-server budget for that server when set (0 / omitted = no per-server cap).

Example: “Higgsfield may burn 2M/month on the intern key; everything else unlimited” →  
`server_policy=all` (or custom including Higgsfield) + `budgets: { srv_higgsfield: 2000000 }`.

## Catalog-miss rule (`tools/list`)

| Key mode | Tool in upstream list but not in our catalog |
|----------|-----------------------------------------------|
| `all`    | **Include** (staleness must not hide tools)   |
| `custom` | **Exclude** (allowlist is explicit)           |

## Denied `tools/call`

JSON-RPC `-32602`: `tool 'X' is not available for this API key` — no upstream hit.  
Activity: `tool_call_denied` (throttled). Server denials: `server_scope_denied` / `server_budget_exceeded`.

## Seeing vs controlling

| | Free | Pro/Team |
|---|------|----------|
| List tools / usage breakdowns | ✓ | ✓ |
| Toggle tools, key server/tool allowlists, set per-server budgets | ✗ (402) | ✓ |

Free 402 body:

```json
{"error":"Tool control is a Pro feature. Set TOKENCONTROLPLANE_LICENSE_KEY for the licensed build."}
```

## Usage accounting

`usage` (per-key) and `usage_by_server` (per-key×server) are updated in **one transaction**.  
Invariant: for each key, `sum(usage_by_server.tokens_used) == usage.tokens_used` for the period.

## Future (non-goals for v1)

- Per-server×tool grant matrix  
- Deny-lists / per-tool rate limits  
- Tool presets (default args)
