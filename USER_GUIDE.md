# TokenControlPlane — Setup & Licensing User Guide

_Untracked reference doc (not committed). Written 2026-09-07 from the live codebase: `pkg/license`, `pkg/billing`, `cmd/gateway`, `cmd/tokencontrolplane-license`, `Makefile`, `docker-compose.yml`._

**What this covers:**
1. [Setup the application](#1-setup-the-application) (dev / binary / Docker)
2. [Generate licenses](#2-generate-licenses-rsa-keys--jwt-keys) (RSA keypair → JWT license keys)
3. [Set up Lemon Squeezy](#3-set-up-lemon-squeezy-automatic-minting) (store → webhooks → auto-mint)
4. [Activate the app — every type](#4-activate-the-app--every-type) (free / manual key / dashboard-billing / customer-side)

**The one mental model:** a *license key* is an offline RS256 JWT. Your **public key** ships inside the binary (embedded); your **private key** stays with you and signs JWTs. Customers paste the JWT into `TOKENCONTROLPLANE_LICENSE_KEY`. No network needed to verify. Missing/invalid/expired key → fail-open to the free tier (3 servers), never a crash.

---

## 1. Setup the application

### 1.1 Developer loop (from source)

Requirements: Go 1.23+, Node 20+.

```bash
cd ~/Desktop/workspace/tokencontrolplane
make dev
# -> gateway   http://localhost:8088  (db: ./dev.db)
# -> dashboard http://localhost:5173  (HMR, /api -> :8088)
# Ctrl-C stops both
```

- Dev DB is isolated (`dev.db`); drop it with `make clean-dev`.
- `make test` = `go test ./...` + frontend vitest.
- `make build` = `npm ci && npm run build` + copy into `pkg/web/dist` (go:embed) + `go build` → `bin/tokencontrolplane` and `bin/tokencontrolplane-license`.

### 1.2 Run the binary (self-hosted)

The binary embeds the dashboard — it serves the SPA and the API on one port.

```bash
./bin/tokencontrolplane --version
JWT_SECRET="<64+ random chars, required in production>" \
ADMIN_TOKEN="<token for the admin API, optional>" \
GATEWAY_URL=https://gw.example.com \
./bin/tokencontrolplane --addr :8080 --db /data/gateway.db
```

Flags: `--addr` (env `ADDR`, default `:8080`), `--db` (env `DB_PATH`), `--version`.
Subcommands: `tokencontrolplane update` (self-update, SHA256-verified), `update --check`, `rollback`.

**Required env in production:**

| Var | Meaning |
|---|---|
| `JWT_SECRET` | Session signing secret. **Required** — empty falls back to a dev secret with a warning. Generate: `openssl rand -base64 48` |
| `TOKENCONTROLPLANE_LICENSE_KEY` | Your paid license JWT (optional → free tier) |
| `GATEWAY_URL` | Public URL (used for return URLs / copy snippets) |
| `ADMIN_TOKEN` | Optional. If set, protects the admin proxy API; if empty, admin API is 401 |
| `DB_PATH` | SQLite file (single file; pure-Go driver, no DB server) |
| `NO_UPDATE_CHECK=1` | Optional. Disables the GitHub update check (air-gap) |

### 1.3 Docker

```bash
# from repo root
export JWT_SECRET="$(openssl rand -base64 48)"
export TOKENCONTROLPLANE_LICENSE_KEY="***"   # if you have a paid key
docker compose up -d --build
# → http://localhost:8080, DB persisted in ./data/gateway.db
```

`docker-compose.yml` already wires `ADDR`, `DB_PATH=/data/gateway.db`, `JWT_SECRET`, `ADMIN_TOKEN`, `GATEWAY_URL`, `TOKENCONTROLPLANE_LICENSE_KEY`, and a `./data:/data` volume.

### 1.4 First login (all setups)

1. Open the dashboard → **Register** (email + password + account name). The first user becomes admin.
2. Setup wizard (Replay tour) walks through adding a server and a key.
3. Add a server (HTTP URL or local stdio command) and create an API key (`tcp_…`) — that key is what your AI tools use against `https://gw.example.com/mcp/…`.

---

## 2. Generate licenses (RSA keys + JWT keys)

A license key is a **JWT signed with your RSA private key**. Two halves:

- **Public key** — already in the repo and embedded in every binary: `pkg/license/license_pub.pem`. Never regenerate it unless you want to invalidate all existing keys.
- **Private key** — you generate it **once**, keep it **off the repo** (`.gitignore` already excludes `*.pem` except the public one).

### 2.1 Generate the RSA keypair (one time, on your laptop)

If you already have the keypair that matches `pkg/license/license_pub.pem`, skip to 2.2. Otherwise:

```bash
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out license_private.pem
diff <(openssl pkey -in license_private.pem -pubout) pkg/license/license_pub.pem
```

- If `diff` is empty → your private key matches the shipped public key. Done.
- If it differs → replace `pkg/license/license_pub.pem` with the new public key (this changes what the binary trusts — old keys stop validating), and commit the public PEM.

Store the private key somewhere safe (1Password file, encrypted disk). **Never commit it, never paste it into the gateway's `.env`.**

### 2.2 Mint a license key (manual path)

```bash
cd ~/Desktop/workspace/tokencontrolplane
make build-go   # → bin/tokencontrolplane-license  (or: go build -o bin/tokencontrolplane-license ./cmd/tokencontrolplane-license)

cat > /tmp/claims.json <<'EOF'
{"sub":"TokenControlPlane","plan":"pro","max_seats":10,"max_servers":10,"max_keys":20}
EOF

./bin/tokencontrolplane-license --private-key license_private.pem --in /tmp/claims.json --days 365
# → eyJhbG… (the license key — send this to the customer)
```

Flags: `--private-key <path>` (or env `TOKENCONTROLPLANE_LICENSE_PRIVATE=<path>`), `--in <claims.json>` (or stdin), `--days N` (default 365; use 730 for a 24-month founders window).

**Claims fields:**

| Field | Meaning |
|---|---|
| `sub` | Customer identifier — use their **purchase email** (or a name like `TokenControlPlane` for internal/demos). Shown in dashboard + logs |
| `plan` | `pro` \| `team`. Caps come from the plan; the explicit numbers below are per-key overrides |
| `max_seats` / `max_servers` / `max_keys` | Optional per-key overrides (0 = use plan defaults) |
| `--days` | Validity → JWT `exp`. **This is the update window.** 365 = standard, 730 = founders |

Verify a key before sending: the gateway prints `TokenControlPlane — licensed plan=… subject=… expires=…` at boot, or decode the payload (unsigned, human-readable): `<key> | cut -d. -f2 | base64 -d 2>/dev/null`.

### 2.3 What the customer does with it

They put the JWT into their deployment (see §4.3) and restart. Boot log:

```
INFO TokenControlPlane — licensed plan=pro subject=customer@example.com max_servers=10 expires=2027-09-07T…
```

### 2.4 Reissue (same plan & expiry, new subject or fresh `iat`)

Via dashboard (Billing → **Reissue key**, optional email override) or API `POST /api/v1/licenses/reissue` — this requires the **billing side** to have the private key configured (`TOKENCONTROLPLANE_LICENSE_PRIVATE`), i.e. the server-side flow (§3). For a purely manual setup, just mint a new one with the CLI (old and new both validate until the old one's `exp` — there's no revocation list in v1).

---

## 3. Set up Lemon Squeezy (automatic minting)

This turns the flow into: **customer clicks Buy → pays on LS → your gateway receives a webhook → signs the JWT → customer sees the key in their dashboard.** LS is merchant of record (VAT/sales tax/refunds).

### 3.1 Create the store & products

1. [lemonsqueezy.com](https://lemonsqueezy.com) → **Add store**.
2. Create a product **"TokenControlPlane Pro"** with one variant → copy the **Variant ID**.
3. Create a product **"TokenControlPlane Team"** with one variant → copy the **Variant ID**.
4. **Webhooks**: Developer → Webhooks → add endpoint `https://your-gateway-url/api/v1/billing/webhook`.
   - Events: **order created/paid** and **order refunded** (the handler accepts `order_created`, `order_paid`, `order_refunded`, `refund_created`, `order.refunded`, `refund.completed`).
   - Copy the **webhook signing secret** (LS shows it once — `X-Signature` = hex HMAC-SHA256 of the raw body).

**Founders pricing:** mint-side founders detection is by **order count** (`TOKENCONTROLPLANE_FOUNDERS_LIMIT`, default 100). The simplest honest approach: run the store at the founders price, and raise the LS price once the count is reached — every minted order under the limit automatically gets `is_founders=1` + the 24-month window. (Refunded founders orders keep their slot — anti-churn, by design.)

### 3.2 Configure the gateway (the seller's instance)

This is the instance *you* run that hosts billing (can be the same box you self-host on). Add to its env:

```env
# Lemon Squeezy
LQ_SECRET_KEY=lq_s…_k…        # Developer → API Keys (secret, server-side)
LQ_WEBHOOK_SECRET=whsec_…     # from the webhook endpoint (verifies X-Signature)
LQ_STORE_ID=12345
LQ_VARIANT_ID_PRO=111111
LQ_VARIANT_ID_TEAM=222222
LQ_RETURN_URL=https://gw.example.com/billing?success=1   # optional (defaults to GATEWAY_URL/billing?success=1)

# License signing (the private key FILE on this host)
TOKENCONTROLPLANE_LICENSE_PRIVATE=/opt/gateway/license_private.pem

# Commercial switches (optional)
TOKENCONTROLPLANE_FOUNDERS_LIMIT=100    # 0 = unlimited, -1 = off (default 100)

# This deployment's own license + runtime
TOKENCONTROLPLANE_LICENSE_KEY=***
JWT_SECRET=…
GATEWAY_URL=https://gw.example.com
```

Billing is **enabled** when `LQ_SECRET_KEY` + `LQ_STORE_ID` + `LQ_VARIANT_ID_PRO` are all set. Restart the gateway after changing env.

**If the private key is missing/unreadable:** orders still get recorded, the account gets `needs_manual_key=1`, and the dashboard shows a "key pending" state — you then mint with the CLI (§2.2) and attach the key via **Reissue** in the dashboard. The webhook still answers 200 (no LS retry storm).

### 3.3 Walk through one real sale (test mode)

1. On the billing host, open the dashboard as a test user → **Billing** → **Buy Pro**.
2. You're redirected to the LS checkout. Use the LS **test card** (available in the LS dashboard) or pay for real.
3. LS fires `order_paid` → the webhook verifies HMAC, dedupes (`webhook_events`), maps variant → plan, resolves/creates the account (by `custom_data.account_id` or buyer email), checks the founders count, picks the window (12/24 months), signs the JWT (`sub` = buyer email), inserts the `licenses` row, clears `needs_manual_key`.
4. Browser returns to `/billing?success=1` → the **Your license key** card shows the JWT (masked, with copy buttons and ready-made `TOKENCONTROLPLANE_LICENSE_KEY=*** / docker `environment:` snippets).
5. Verify: `sqlite3 gateway.db "select plan from accounts"` shows `pro`; a gateway booted with the key logs `licensed plan=pro`.
6. **Refund test:** refund the order in LS → webhook revokes the license row and downgrades the account to free.

### 3.4 Operations notes

- **Idempotent:** webhook retries are safe (event id = `order_paid:<order_id>`).
- **Order lookup:** `lq_order_id` on `licenses` is unique; refunds find the license by it.
- **Renewal (v1):** customer buys the same plan again at full price → new license row, `exp = new purchase + window` (**not** stacked onto the old expiry). The dashboard's renew button uses the same checkout.
- **Portal:** `POST /api/v1/billing/portal` returns the LS customer portal URL (order history / invoices) once the account has purchases.

---

## 4. Activate the app — every type

### 4.0 Type: FREE tier (no key at all)

Do nothing. Boot log: `unlicensed free tier (3 servers)`.

- 3 servers · 3 seats · 5 API keys · 5M tokens/mo per key · no per-tool policy.
- Hitting a limit → the dashboard says "Free-tier limit reached (3 servers). Set TOKENCONTROLPLANE_LICENSE_KEY…".
- Perfect for trials and small setups; everything works, no clock running.

### 4.1 Type: MANUAL license (you mint, customer pastes) — no Lemon Squeezy

The simplest paid setup. No webhook infra.

1. You sell (invoice however you like), then mint (§2.2) with `sub` = their email and `--days` per the deal (365 standard / 730 founders).
2. Send them the JWT.
3. They activate (§4.3) and restart. Done.

Activation is verified by the boot log line `TokenControlPlane — licensed plan=pro …`.

### 4.2 Type: DASHBOARD BILLING (Lemon Squeezy auto-mint)

You run one **billing host** with the env from §3.2. Two sub-cases:

**(a) Customer registers on your instance, buys from the dashboard**
1. Customer: Register → Billing → **Buy** → LS checkout → success.
2. The key appears in *their* dashboard (Billing page, masked + copy).
3. If their gateway is *the same deployment* (you host it for them), it's already active — the account plan applies server-side.
4. If they **self-host their own gateway**, they copy the key and activate per §4.3.

**(b) Customer buys from the standalone store page** (no account yet)
1. They pay on LS. The webhook creates an account for the buyer email.
2. They register on your instance with that email → the plan + key are already attached.

### 4.3 The customer-side activation (every paid path ends here)

**env / systemd:**
```ini
Environment="TOKENCONTROLPLANE_LICENSE_KEY=***"
```

**docker compose:**
```yaml
environment:
  TOKENCONTROLPLANE_LICENSE_KEY: "***"
```

Then restart. The dashboard (if they use yours) shows the same key under Billing → **Your license key** with copy snippets, so they rarely have to hunt for it.

**Confirming activation:**
- Boot log: `INFO TokenControlPlane — licensed plan=pro subject=… max_servers=10 expires=…`
- `GET /api/v1/licenses/me` (authed) → `plan`, `key`, `expired:false`, `days_to_expiry`, `ever_works_forever:true`.
- Limits lifted: they can add servers past 3 without a 402.

---

## 5. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Boot log: `LICENSE KEY INVALID OR EXPIRED — falling back to free tier` | Key typo, wrong public key in binary, or `exp` passed. Re-issue; check `diff` of pub keys if you regenerated. |
| Boot log: `unlicensed free tier (3 servers)` | `TOKENCONTROLPLANE_LICENSE_KEY` empty in env (check the unit file, not just your shell). |
| 402 "Plan limit reached. Upgrade…" | You're on a capped plan at its limit — expected. Upgrade to Pro/Team for higher caps. |
| 402 "Key 'X' hit monthly budget" | Per-key `monthly_budget` (free-tier default 5M) — it's an operator control; set it to 0 to disable. Unrelated to the plan's token cap. |
| Paid order, but no key in dashboard | `needs_manual_key=1` — private key not set on the billing host (`TOKENCONTROLPLANE_LICENSE_PRIVATE`), or webhook failed HMAC (`LQ_WEBHOOK_SECRET` mismatch). Check gateway logs for `ls webhook` lines. |
| Webhook 400 "invalid signature" | `LQ_WEBHOOK_SECRET` doesn't match the LS endpoint secret; or a proxy is modifying the body (LS signs the **raw** bytes). |
| Webhook events arriving but nothing happens | Event name not in the accepted set, or `order` status not `paid` (pending orders are ignored — that's correct). |
| "billing not configured — set LQ_SECRET_KEY…" | One of the three enabling vars is missing on the host serving the dashboard. |
| Admin API 401 | `ADMIN_TOKEN` empty — set it (admin proxy endpoints are 401 by design when unset). |
| Expired key but gateway "still licensed" | The JWT `exp` is checked at **boot only** (fail-open design). Restart to re-evaluate. The *update window* (dashboard banner) is a separate, server-side notion. |
| Renewal didn't extend the window | Renewals mint a **new** row with `exp = new purchase + window`; they never stack. Old row stays until the new one is purchased. |

---

## 6. Security checklist (before going public)

- [ ] Private key **never** in the repo (only `pkg/license/license_pub.pem` is tracked; `.gitignore` excludes `*.pem`/`*.key`).
- [ ] Git history swept for `.env` and any committed private key (before the first public push).
- [ ] `JWT_SECRET` set to a strong random value in every deployment (never the dev fallback).
- [ ] `LQ_WEBHOOK_SECRET` set (webhook HMAC verification active) — the endpoint must be **HTTPS-only**.
- [ ] `LQ_SECRET_KEY` is a *secret* (server-side) LS key, not a public one.
- [ ] `TOKENCONTROLPLANE_LICENSE_PRIVATE` file has `600` perms, owned by the gateway user.
- [ ] `ADMIN_TOKEN` set if you use the admin proxy API.
- [ ] Backups: `gateway.db` (+ `-wal`/`-shm`) — it's the single source of truth for accounts, licenses, keys, usage.
