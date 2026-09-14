# Releasing TokenControlPlane

Operator manual for cutting a release from a laptop (tag → CI → publish → verify → announce).

## Versioning policy

- **SemVer 1.x:** `MAJOR.MINOR.PATCH`.
- **MINOR** — new feature phase (every phase prompt → a minor bump when that work ships).
- **PATCH** — fix without new capability.
- **Pre-releases** (`v1.1.0-rc.1`) allowed; `VersionLess` treats `-rc` / pre-release suffixes as **less than** the matching release (`1.0.0-rc1 < 1.0.0`).
- **Migrations are append-only** (no down-migrations). Upgrade contract: **forward-only**. Auto-update may advance schema on next boot. `tokencontrolplane rollback` **refuses** if schema advanced beyond the previous binary — restore a DB backup or keep the newer binary. The dashboard update path **never** auto-rollbacks (that would be a data trap after a forward migration).
- **License window is version-independent.** Caps never change with binary version; only the update *entitlement* (window expiry) gates which version the dashboard recommends. Do not compute caps from version strings.
- **`CHANGELOG.md` is the source of release notes.** Every release lands its changelog section in the **same PR/commit as the code**. CI fails if the tag’s section is missing.

## Division of docs

| Doc | Role |
|-----|------|
| `CHANGELOG.md` | Durable user-visible history (Keep a Changelog). |
| `RELEASE_NOTES.md` | Pre-tag **checklist** (tests, smoke, screenshots) — not the public notes. |
| `docs/RELEASING.md` | This process. |
| GitHub Release body | Extracted from `CHANGELOG.md` by CI. |

## Cut a release (happy path)

1. `git checkout main`, working tree clean, all phase work committed.
2. Locally: `go test ./...` and `make build` green.
3. Move `[Unreleased]` items into a new `## [X.Y.Z] - YYYY-MM-DD` section in `CHANGELOG.md` (what/why, user-visible only; link PRs when useful). Leave an empty `[Unreleased]` stub on top.
4. Update `VERSION` to `X.Y.Z` (no leading `v`). Tick/complete relevant boxes in `RELEASE_NOTES.md` (or note gaps in `KNOWN_LIMITS.md`).
5. Commit: `phaseN: release — vX.Y.Z` (or a dedicated release commit).
6. Tag and push to the public repo ([github.com/devthinker-ai/TokenControlPlane](https://github.com/devthinker-ai/TokenControlPlane)):
   ```bash
   git tag vX.Y.Z
   git push origin main
   git push origin vX.Y.Z
   ```
7. **One-time GHCR setup** (required — without this, Docker push fails with `permission_denied: write_package`):
   1. Create a **classic** PAT: https://github.com/settings/tokens/new  
      Scopes: `write:packages`, `read:packages` (and `delete:packages` optional).
   2. Repo → **Settings → Secrets and variables → Actions** → New repository secret:  
      Name `GHCR_TOKEN`, value = the PAT.
   3. Also set **Settings → Actions → General → Workflow permissions** → **Read and write permissions**.
   4. If an old orphaned package blocks pushes: https://github.com/users/devthinker-ai/packages → delete `tokencontrolplane` if present, then re-run.
8. CI (`.github/workflows/release.yml`) will:
   - `npm ci && npm run build`
   - `go vet ./...` + `go test ./...`
   - Extract changelog section for the tag (**fails** if missing)
   - Cross-compile 4 platforms + `SHA256SUMS`
   - `gh release create` with that section as notes (idempotent on re-run)
   - Push multi-arch Docker image to `ghcr.io/devthinker-ai/tokencontrolplane:vX.Y.Z` **and** `:latest` (auth via `GHCR_TOKEN`)
9. Verify:
   ```bash
   gh release view vX.Y.Z          # assets + notes from CHANGELOG
   docker pull ghcr.io/devthinker-ai/tokencontrolplane:vX.Y.Z
   # Product claim — self-update path:
   # from an older install (or UPDATE_URL mirror):
   tokencontrolplane update --check      # exit 1 if newer
   tokencontrolplane update              # or update --file … --sha256 …
   tokencontrolplane version
   ```
10. Announce the release (changelog + GitHub Release notes).

### Docker vs binary

- **Bare-metal / systemd:** use `tokencontrolplane update` (or the dashboard **Update now** button). That path is the product claim. Default release source is `GITHUB_REPO=devthinker-ai/TokenControlPlane`.
- **Containers:** no in-container binary swap — re-pull and recreate:
  ```bash
  docker pull ghcr.io/devthinker-ai/tokencontrolplane:latest
  # recreate your compose/service
  ```

### Cron (optional)

```bash
# exit 1 when newer exists — wire to mail/notify; do not auto-apply without an operator
0 4 * * * NO_UPDATE_CHECK=0 /usr/local/bin/tokencontrolplane update --check || true
```

Air-gapped hosts: set `NO_UPDATE_CHECK=1` (banner hidden; `/api/v1/version.update_available=false`).

## Hotfix

1. Branch `hotfix/vX.Y.(Z+1)` from the release tag.
2. Fix + add a `## [X.Y.Z+1]` changelog section.
3. Tag `vX.Y.Z+1` and push — same CI path.
4. Merge the hotfix branch back to `main`.

## When to force a MAJOR

Breaking API/CLI/dashboard-auth changes, or a migration that is **not** append-only-safe (should not happen). If it does, document the migration/restore path in the changelog and bump MAJOR.

## CI regression notes (Phase 15)

Two bugs fixed in `release.yml` — keep them fixed:

1. **`latest` image tag:** `type=raw,value=latest` with **no** `enable={{is_default_branch}}`. On tag pushes `is_default_branch` is false, so the old condition never tagged `latest`. Policy: **every release tag updates `latest`** (including patches).
2. **Tests before publish:** `go vet` + `go test` run **before** cross-compile. A broken release is unrecoverable for customers (tags are immutable).

Changelog extraction: `scripts/extract-changelog.sh` (also covered by `pkg/update` unit tests). Missing section → fail with `CHANGELOG.md has no entry for vX.Y.Z — add one`.

## Dashboard update UX (summary)

- Footer shows `vX.Y.Z · schema N`; click for commit/built + copy.
- Banner states: update available + window active → **Update now**; available + expired → renew CTA (current keeps working); none / `NO_UPDATE_CHECK` → silent.
- Admin **Run on this host** → `POST /api/v1/update` (same verify + `.prev` + systemd as CLI). Rollback remains deliberate CLI only.

## Preset catalog probe (Phase 16)

Before tagging a release that adds/changes MCP or LLM presets in
`frontend/src/data/catalog.ts`, re-verify remote HTTP entries:

```bash
./scripts/probe-presets.sh
```

The script POSTs MCP `initialize` to every `verified: true` HTTP preset and
expects **200 / 401 / 406 / 307**. Unexpected statuses → fix the URL or clear
`verified` before tagging. Not part of CI (network + vendor flakiness); run by
hand. Stdio presets are not probed (package publish + documented command line).

`GET /api/v1/servers/catalog` is intentionally deferred — the catalog ships
inside the SPA for air-gap installs; a server-driven endpoint can mirror it later.
