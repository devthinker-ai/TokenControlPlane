# Changelog

All notable changes to TokenControlPlane are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versioning policy (also in [docs/RELEASING.md](docs/RELEASING.md)):

- **MINOR** = new feature phase (each phase prompt → a minor bump at release).
- **PATCH** = fix without new capability.
- Migrations are **append-only / forward-only**; rollback refuses if schema advanced.
- License caps are **version-independent**; only the update window gates which version the dashboard recommends.

## [Unreleased]

### Changed

- Public GitHub home: [github.com/devthinker-ai/TokenControlPlane](https://github.com/devthinker-ai/TokenControlPlane).
  Release/update defaults use `GITHUB_REPO=devthinker-ai/TokenControlPlane`;
  Docker image `ghcr.io/devthinker-ai/tokencontrolplane`.
- Pricing ladder is Free / Pro / Team only (Enterprise tier removed).

### Added

- Multilingual console (DE/EN): `i18next` + locale switcher, persisted `tcp:lang`,
  Intl-aware dates/numbers; API error payloads stay English verbatim.
- Dashboard 2FA (TOTP + recovery codes) for admin/member login; machine keys unchanged.
  Enroll/remove from Settings; MFA step on login; migration `0011_totp.sql`.
- Preset catalog + Quick-add onboarding: one-click verified MCP servers and LLM
  providers (`frontend/src/data/catalog.ts`), guided empty states, SetupWizard
  start-here cards, and fix-it error hints. Re-verify remotes with
  `./scripts/probe-presets.sh` before tagging ([docs/RELEASING.md](docs/RELEASING.md)).
- In-dashboard version footer + dialog (`GET /api/v1/version` joins build metadata with license update window).
- Decision-aware update banner: update now (window active) vs renew (window expired; current version keeps working) vs silent (air-gap / no update).
- Admin server-side update (`POST /api/v1/update`) with SHA256 verification, `.prev` backup, systemd-aware restart; release notes proxy (`GET /api/v1/releases/{tag}`).
- `CHANGELOG.md` + `docs/RELEASING.md`; CI extracts changelog section as GitHub release notes; `go test`/`go vet` before publish; `ghcr …:latest` tagged on every release.
- LLM providers + OpenAI-compatible model routing (`POST /v1/chat/completions`), per-provider budgets, route aliases, pre-stream fallback ([docs/LLM_ROUTING.md](docs/LLM_ROUTING.md)).

## [1.0.0] - 2026-09-05

### Added

- Transparent MCP reverse proxy with SSE flush, metering (bytes/4), circuit breaker, kill switch, and per-request budget enforcement.
- Dashboard (embedded SPA): setup wizard, servers, keys, activity feed, license/billing.
- Auth: machine keys (`tcp_*`) separate from JWT user sessions; argon2id + RS256 offline licenses (fail-open free tier).
- Lemon Squeezy one-time licenses (no subscription); perpetual use with time-boxed update window.
- Numbered SQLite migrations, `tokencontrolplane update` / `rollback`, Docker / Homebrew / install.sh packaging.
- Upstream OAuth (device flow + PKCE) ([docs/UPSTREAM_AUTH.md](docs/UPSTREAM_AUTH.md)).
- Tool catalog + per-key/server tool policy ([docs/TOOL_POLICY.md](docs/TOOL_POLICY.md)).
- Stdio transport for local MCP processes ([docs/STDIO.md](docs/STDIO.md)).
- Team seats, invites, roles, Members page ([docs/TEAM.md](docs/TEAM.md)).
- Founders offer + pricing ladder; dashboard UI refresh.
