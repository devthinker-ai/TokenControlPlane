# Known limits (v1)

Honest gaps — do not pretend these ship working.

| Item | Status |
|------|--------|
| Latency badge on Servers | Deferred — `last_latency_ms` not persisted without `pkg/proxy` instrumentation |
| Mid-stream token kill | Deferred to v1.1 (oversized response completes; next request 402) |
| OAuth gateway→upstream | **Shipped Phase 10** — device flow + PKCE; see [docs/UPSTREAM_AUTH.md](docs/UPSTREAM_AUTH.md) |
| Stdio / local MCP spawn | **Shipped Phase 11b** — see [docs/STDIO.md](docs/STDIO.md). Remaining: Windows spawn, per-child cgroup limits |
| Phone-home license validation | Intentionally omitted (honor system) |
| `?key=` URL auth | Not in v1 (Bearer header only) |
| Setup wizard E2E screenshots | Phase 5; checklist open in RELEASE_NOTES until done |
| Latency p50 number | Measure on release hardware; fill RELEASE_NOTES |
| Docker multi-arch publish | CI pushes `ghcr.io/devthinker-ai/tokencontrolplane` on tag (see [docs/RELEASING.md](docs/RELEASING.md)) |
| Upstream auth encryption-at-rest | Auth values plaintext in SQLite in v1 (MCP servers **and** LLM providers) |
| LLM fallback mid-stream | Pre-stream only; mid-stream failures surface as 502 with partial bytes |
| Embeddings / `/v1/models` proxy | Not in v1 — chat completions only; health checks hit upstream `/models` on demand |
| LLM usage parsing | Trusts OpenAI-compatible `usage` fields; non-conformant → bytes/4 |
| Cost-per-token pricing | Not in v1 (Phase 16+) |
| Forward-only migrations | No down-migrations in v1; `rollback` refuses if schema > binary |
| Discounted renewal (~45–50%) | Deferred Phase 13 commercial non-goal / later billing phase — v1 renewal = full plan price |
| Role change / removal JWT | Applies at **next login** — stateless sessions, no server-side invalidation in v1 |
| Invite email delivery | Code/link only in v1; SMTP is a later phase |
| Update binary `.prev` | Single generation only; second update drops older backup |
| Signed releases / cosign | Deferred — no key management yet; SHA256SUMS verification only |
| Stable/beta update channels | Not in v1 — single latest; cron `update --check` documented in [docs/RELEASING.md](docs/RELEASING.md) |
| Docker self-update | Containers re-pull; binary updater is bare-metal/systemd only |
| Wizard chips: Postgres & Fetch | **Placeholder URLs** (`mcp.example.com`) — indexing will fail by design until real endpoints are configured; GitHub/Slack chips are real. Swap in a genuinely public fetch MCP (or make chips editable-only) before launch. |

If a checklist box in RELEASE_NOTES cannot be verified before tag, move it here rather than marking it done.
