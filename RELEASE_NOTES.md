# Release notes — v1.0.0 (checklist)

Track verification before tagging. Anything not verified goes to [KNOWN_LIMITS.md](KNOWN_LIMITS.md).

## Build & tests

- [x] `go test ./...` green (run locally before tag)
- [x] `npm run build` green (`make build` / frontend)
- [x] Docker image builds on Apple Silicon (`docker build -t tokencontrolplane .` — verified 2026-09-05, image sha256:a04329…)
- [x] `CGO_ENABLED=0` build produces a runnable binary (`make build-go`)

## End-to-end smoke (manual — launch day)

- [ ] Fresh binary → setup wizard on first login (Phase 5) → complete wizard
- [ ] Claude Desktop connects via gateway URL + key
- [ ] `tools/list` shows indexed tools
- [ ] Tool call is metered on dashboard
- [ ] Kill key → 402 body → unkill → works again
- [ ] Screenshots for PH (wizard Done = hero #1)



## Latency

- [ ] `curl -w` proxied vs direct upstream — gateway p50 add <5ms  
  *Report number here when measured:* **+0.04ms p50, +0.23ms p90** (measured 2026-09-05, M2 local, 10 samples each, loopback upstream — comfortably inside the <5ms claim)



## Binary / Alpine

- [x] Binary size recorded: **20 MB** (`ls -lh bin/tokencontrolplane`, 2026-09-05)
- [ ] Distroless/Alpine run: no glibc errors (`Dockerfile` uses distroless static)



## Security pass

- [x] No plaintext client keys in access logs (hash-only storage; middleware does not log Bearer)
- [x] SQLite file perms 0600 on open
- [x] Lemon Squeezy webhook signature check when `LQ_WEBHOOK_SECRET` set (Phase 7; Stripe removed)
- [x] Dashboard embedded same-origin; no broad CORS by default  
  *Note:* `Access-Control-Allow-Origin: `* is **not** enabled; document if a browser client needs it later.



## Legal / publish

- [x] `LICENSE` Apache-2.0
- [x] Public GitHub: [github.com/devthinker-ai/TokenControlPlane](https://github.com/devthinker-ai/TokenControlPlane)
- [ ] Tag `v1.0.0`
- [ ] Publish Docker image to GHCR (`ghcr.io/devthinker-ai/tokencontrolplane`)
- [ ] Announce (PH, HN, registries)



## Packaging delivered in this phase

- Embedded SPA (`pkg/web` + Makefile sync from `frontend/dist`)
- Offline RS256 licenses (`pkg/license`, `cmd/tokencontrolplane-license`, fail-open free tier)
- Dockerfile + docker-compose + README quickstart
- Numbered migrations (`pkg/store/migrations`), binary versioning (ldflags), `tokencontrolplane update` / rollback
- KNOWN_LIMITS.md, this checklist

---



## Phase 7 — Lemon Squeezy one-time licenses (checklist)

- [x] `go test ./...` + `npm run build` green (re-verified with Phase 17)
- [x] No Stripe SDK/import/env in `go.mod` / `go.sum` / `pkg/` / `cmd/` (only historical `stripe_*` columns + rename in migrations `0001`/`0002` — allowed)
- [x] Webhook HMAC: bad `X-Signature` → 400; good signature accepted (`pkg/billing` tests)
- [x] Mint path: `order_created`/`order_paid` → license row + JWT validates (`TestWebhookIdempotencyAndMint`)
- [x] Refund → license revoked + plan → free (`TestRefundRevokesLicense`)
- [x] Webhook replay → `{"ok":true,"duplicate":true}`
- [x] Real payload shape documented in `pkg/billing` header: `POST /v1/checkouts`, `X-Signature`, `order_created` / `order_refunded`, `user_email`, `LQ_STORE_ID`
- [ ] Live LS test event against a real store (manual)
- [ ] Live e2e: checkout → webhook → key on License page → paste into `TOKENCONTROLPLANE_LICENSE_KEY`
- [ ] Portal URL opens for an account with `ls_customer_id` (manual; handler + 503-when-missing covered in code)

---



## Phase 8 — Dashboard UI refresh (frontend only)

- [x] Light theme default + dark class via `prefers-color-scheme`
- [x] Design tokens + `@fontsource-variable/inter` (self-hosted)
- [x] Table primitive, shell (w-60 sidebar + brand lockup), page header pattern
- [x] Overview / Servers / Keys / Activity / License restyled; no `window.confirm`
- [x] `npm run build` + `npm test` green; SPA synced to `pkg/web/dist`
- [ ] Screenshot pass → `frontend/screenshots/phase8/` (manual)
- [ ] Lighthouse login ≥90 perf / ≥95 a11y (manual)

---



## Phase 10 — Upstream OAuth (device flow + PKCE)

- [x] `go test ./...` green (incl. `pkg/auth/upstream` Higgsfield + RFC8628 + slow_down + refresh + PKCE)
- [x] `npm run build` green; SPA synced to `pkg/web/dist`
- [x] Migration `0003_upstream_oauth` applied; schema version 3
- [ ] Live e2e: add `https://mcp.higgsfield.ai/mcp` with oauth_device → Connect → sign-in → tools/list → proxy tool call → restart persists → force-expire refreshes → bad refresh → reconnect needed + 401
- [x] Static servers unchanged (`proxy_test.go` key injection — suite green)
- [x] `docs/UPSTREAM_AUTH.md` present; AGENTS.md invariant 12

---



## Phase 11a — Tool catalog + tool policy

- [x] Migration `0004_tool_policy`; `pkg/policy` cache; Pro `ToolPolicy` cap
- [x] `GET/PATCH …/servers/{id}/tools`, bulk, key tool access APIs
- [x] Proxy: denied `tools/call` (0 upstream); `tools/list` filter; batch mixed
- [x] Servers drawer + Keys “Tool access” UI; free 402 gate
- [x] `docs/TOOL_POLICY.md`
- [x] Migration `0006`: `server_policy`, `key_server_grants`, `usage_by_server`, `key_server_budgets`
- [x] Proxy server-scope 403 + per-server budget 402; metering dual-write
- [x] Key detail drawer: Servers / Tools / Usage
- [ ] Live e2e (Higgsfield 101 tools; key custom servers + per-server budget)



### Phase 11b — Stdio transport

- [x] Migration `0005_stdio_transport`; `pkg/upstream/stdio` ProcessManager + bridge
- [x] Indexer + proxy route for `transport=stdio`; policy filter shared with 11a
- [x] API: create/patch/status/check-command/parse-mcp-json; graceful CloseAll
- [x] Frontend: transport segment, paste-to-convert, process panel
- [x] `docs/STDIO.md`; KNOWN_LIMITS updated
- [ ] Live demo: `npx -y @modelcontextprotocol/server-filesystem /tmp` via paste

---



## Phase 12 — Commercial model (perpetual + upgrades)

- [x] Migration `0007_commercial` (`window_months`, `is_founders`)
- [x] `pkg/billing/pricing.go` ladder; `GET /api/v1/pricing`; founders limit env
- [x] Webhook mint uses window/founders; refund keeps founders slot
- [x] `GET /licenses/me` → `ever_works_forever`, window, founders fields
- [x] BillingPage: fetch pricing, founders chip, expired/expiring “keeps working” copy
- [x] README one-liner
- [ ] Manual screenshots 1440/375 light+dark (active / founders / expiring / expired)

---



## Phase 13 — Team management

- [x] Migration `0008_team` (invites + `api_keys.owner_id`)
- [x] Users/invites/join APIs; `RequireAdmin` gating; key attribution
- [x] Members page + register `?invite=` join; keys creator chip; seats on me/Billing
- [x] `docs/TEAM.md`; KNOWN_LIMITS / LAUNCH / COMMERCIAL updates
- [ ] Manual screenshots: Members, join flow, keys chip

---



## Phase 15 — Versioning & release system

- [ ] `go test ./...` + `npm run build` / `npm test` green
- [ ] `CHANGELOG.md` seeded; `scripts/extract-changelog.sh v1.0.0` prints section; missing tag fails
- [ ] `docs/RELEASING.md` covers tag → CI → verify → announce + hotfix + forward-only contract
- [ ] `GET /api/v1/version` shape + update_window active/expired/null; member 200
- [x] Dashboard version footer + About dialog (build hero, update status, human-readable built/commit, copy details)
- [ ] Dashboard banner states (update / renew / none)
- [ ] Admin `POST /api/v1/update` swaps binary + `.prev`; SHA mismatch leaves binary unchanged
- [ ] `GET /api/v1/releases/{tag}` 24h meta cache
- [ ] `release.yml`: tests before compile; changelog notes; `:latest` without `is_default_branch`
- [ ] Manual: version dialog + banner screenshots (light/dark)

---



## Phase 16 — Onboarding UX + preset catalog

- [x] Preset catalog (`frontend/src/data/catalog.ts`) + Quick-add empty states / wizard start-here
- [x] Fix-it error hints; probe script for remotes (`scripts/probe-presets.sh`)
- [x] `go test ./...` + `npm test` / `npm run build` green (verified with Phase 17)
- [ ] Manual: wizard / Quick-add screenshots light+dark
- [ ] Re-verify remotes with `./scripts/probe-presets.sh` before tag

---



## Phase 17 — Unlimited tokens (Pro/Team)

*No DB migration — plan names are TEXT; caps live in* `license.CapsForPlan`*. Enterprise tier was also introduced here and **removed in Phase 21** (ladder is Free / Pro / Team).*

### Caps & enforcement

- [x] Pro/Team `MonthlyTokens: 0` (unlimited tokens by default); free tier still 5M
- [x] Per-key / per-server / per-provider budgets still optional (`0 = unlimited`)
- [x] `store.PlanCaps` / `PlanMaxKeys` + `effectiveCaps` stay in sync



### Commercial stack

- [x] Checkout whitelist `pro|team`; founders slot shared across plans
- [x] `GET /api/v1/pricing` returns Pro/Team with `monthly_tokens:0`
- [x] `tokencontrolplane-license` accepts `plan: "pro"` | `"team"`



### Frontend & docs

- [x] Pricing fallback + BillingPage: Unlimited tokens (Pro/Team)
- [x] Pricing card UX: Recommended pill, founders price next to list price, no LS mention in footer
- [x] Key drawer: Monthly token cap presets + affixed input (servers/providers)
- [x] README updated for unlimited Pro/Team tokens



### Tests / verify

- [x] `go test ./...` + `go vet ./...` green
- [x] `npm test` + `npm run build` green
- [ ] Manual screenshots light/dark of License pricing (Free / Pro / Team)

---





