# Team access — seats, invites, roles

How multiple humans share one self-hosted TokenControlPlane account.

## Roles (v1)

| Capability | admin | member |
|------------|-------|--------|
| Overview, servers list, providers/models list, activity, usage | ✓ | ✓ |
| Create / edit / delete servers, OAuth, tool catalog edits | ✓ | |
| Create / edit / delete LLM providers & model routes | ✓ | |
| Create API keys | ✓ | ✓ (stamped as owner) |
| See all keys (team-wide) | ✓ | ✓ |
| Edit / delete **own** keys + budgets / policy | ✓ | ✓ |
| Edit / delete **others'** keys | ✓ | |
| Kill any key (emergency) | ✓ | ✓ |
| Members / invites | ✓ | |
| License / billing | ✓ | |

## Invites (no SMTP in v1)

1. Admin opens **Members** → **New invite**.
2. Gateway returns a one-time code + path `/register?invite=CODE` (and `url` when `GATEWAY_URL` is set).
3. Admin copies the link into Slack / QR / chat.
4. Colleague opens the link → join form (name, email, password) → `POST /join`.
5. Code is single-use, **7-day TTL**, max **5 pending** invites per account.

`GET /api/v1/invites/public/{code}` is the only public invite endpoint; it returns `{account_name, role}` only (no ids/emails).

## Seats

**Seats = humans in the dashboard**, not API consumers. One person can hold many keys; traffic is scoped by keys. The seat cap (`max_seats` from the plan: free 3 / Pro 10 / Team 25) is enforced **at join time** (409), never on proxy requests.

## Flat key / named-team license

The Lemon Squeezy license is **account-level**. Adding a colleague never changes the price. API keys are shared across the team (`api_keys.account_id`); `owner_id` is attribution only. Removing a member clears `owner_id` but **keeps keys working**.

## Known limits

- Role changes and removals apply at **next login** (stateless JWT — no server-side session kill in v1).
- No email delivery of invites (Phase 14+ / SMTP).
- Admin invites via API stay member-only in v1 (column exists for later).
