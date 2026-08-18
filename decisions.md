# Decision Log

Architectural decisions for GoShopping, newest last. Each entry records why a
choice was made, so the reasoning survives past the conversation that produced it.

**Status values:** `Accepted` · `Superseded by ADR-XXX` · `Rejected`

---

### ADR-001 — Frontend is React + Vite + React Router, not Next.js
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** The initial spec called for Next.js with the App Router. The project's
purpose is practicing Go and React; the app is a client-rendered SPA talking to a
separate Go API, with no SSR or SEO requirement.

**Decision.** React 19 + Vite + React Router v7, TypeScript throughout.

**Consequences.** Client-side rendering only, which makes `localStorage` token
hydration straightforward — no server/client boundary to reason about. Loses SSR,
which would matter for public product pages later; revisit if SEO becomes a goal.

---

### ADR-002 — No GraphQL in Step 1
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** The original spec asked for `gqlgen` alongside REST, but no Step 1
deliverable consumed it.

**Decision.** REST only. Revisit when there is a domain worth querying (products,
cart) where GraphQL's shape actually pays for itself.

**Consequences.** Removes generated-code surface from the initial build.

---

### ADR-003 — Hand-written SQL over pgx, no ORM
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** The spec allowed an ORM, a SQL builder, or `database/sql`/`pgx`.

**Decision.** `pgx/v5` with SQL written by hand, behind repository interfaces.

**Consequences.** More explicit code and more instructive for learning Go; the
service layer stays testable against repository interfaces. More boilerplate per
query than an ORM would need.

---

### ADR-004 — Migrations via golang-migrate
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** `golang-migrate/migrate/v4` with numbered `.up.sql`/`.down.sql`
pairs in `backend/migrations/`, applied on boot in development and runnable
standalone.

**Alternatives rejected.** `goose` (equivalent, less common); embedding schema in
Go (loses plain reviewable SQL).

---

### ADR-005 — MUI and Tailwind v4 coexist via CSS cascade layers
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** Tailwind's preflight and MUI's baseline both emit resets and fight
over specificity, producing overrides that are hard to diagnose after the fact.

**Decision.** Declare `@layer mui, theme, base, components, utilities;` and place
MUI's emitted styles in the `mui` layer, configured at root mount. Exact API to be
verified against MUI v9's shipped types rather than assumed.

**Consequences.** Tailwind utilities reliably win over MUI defaults. Set up before
any UI is written, since retrofitting means re-auditing every component.

---

### ADR-006 — Email uniqueness enforced with `citext`
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** Store `users.email` as `citext` with a unique constraint.

**Consequences.** `User@x.com` and `user@x.com` cannot both register, enforced by
the database rather than by application code that a future path might bypass.
Requires `CREATE EXTENSION citext` in the first migration.

---

### ADR-007 — Stateful sessions, with JWT as bearer format
**Date:** 2026-08-15 · **Status:** Accepted — supersedes the "stateless JWT" line in project-setup.md

**Context.** Two requirements conflict: 24-hour access tokens, and password change
invalidating every existing token immediately. A stateless JWT is valid until it
expires precisely because nothing is consulted at verification time. All three of
stateless / long-lived / instantly revocable cannot hold at once.

**Decision.** A `sessions` table is the source of truth. Each row is one login
session holding the current access-token `jti` and a hash of the current refresh
token. The JWT middleware verifies the signature, then validates the session row.

**Consequences.** One indexed lookup per authenticated request; auth is no longer
stateless. In exchange, revocation is immediate and per-device, which also enables
a future "active sessions" view. The signature still earns its place: forged tokens
are rejected before touching the database, and claims avoid a join.

**Alternatives rejected.** A `token_version` integer on `users` — cheaper (no extra
writes, nothing to reap) but all-or-nothing, with no path to per-device sign-out.
A Redis denylist — same statefulness, extra infrastructure.

---

### ADR-008 — 24h access tokens, 30d opaque refresh tokens rotated in place
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** Access token: JWT HS256, `JWT_ACCESS_TTL`, default 24h, carrying a
random `jti`. Refresh token: 32 random bytes, base64url, `JWT_REFRESH_TTL`, default
30d. Refreshing rotates both and updates the same session row, so `sessions.id`
stays stable for the life of the session.

**Consequences.** Only the SHA-256 of the refresh token is stored — the opaque
string is itself the credential, so a database leak must not yield usable tokens.
The access token's `jti` is stored in the clear; it is an identifier, not a
credential, and the signed JWT is what actually grants access.

---

### ADR-009 — Refresh reuse detection via `previous_refresh_hash`
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** Rotating in place overwrites `refresh_hash`, so a replayed old token
matches no row and is indistinguishable from garbage.

**Decision.** Keep one generation of history. A token matching
`previous_refresh_hash` is a genuine replay — revoke every session for that user.

**Consequences.** Catches the realistic attack, where an attacker presents a token
the legitimate client has already rotated past. Detection is one generation deep,
not full history; that would require append-only token rows.

---

### ADR-010 — Password change revokes all sessions and returns a fresh token pair
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** Revoking everything on password change would also sign out the device
that just changed the password, which reads as a bug to the user.

**Decision.** In one transaction: verify the current password, update the hash,
revoke all of the user's sessions, then create one new session. Respond `200` with
the new token pair rather than `204`.

**Consequences.** Every other device is signed out immediately; the current tab
swaps its tokens and continues uninterrupted.

---

### ADR-011 — Tokens in `localStorage`, behind a single seam
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** Both tokens live in `localStorage`, accessed only through
`lib/tokenStorage.ts`, and are hydrated into Redux before first render.

**Consequences.** Simple, and it suits a pure SPA. `localStorage` is readable by
any XSS on the origin, and the 30-day refresh token is the more valuable target.
The stronger arrangement — refresh token in an httpOnly cookie, access token in
memory — needs `credentials: 'include'` plus CORS `AllowCredentials`. Confining all
access to one module keeps that migration to a single file.

---

### ADR-012 — Single-flight refresh in the API client
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** Several requests failing with `401` at once would each trigger a
refresh. With rotation, the second and third would present an already-revoked
token, trip reuse detection (ADR-009), and sign the user out.

**Decision.** The refresh call is a shared promise; concurrent callers await the
same in-flight request, then replay their original requests.

**Consequences.** Easy bug to ship and unpleasant to diagnose, so it is built in
from the start rather than retrofitted.

---

### ADR-013 — One default address enforced by a partial unique index
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** `CREATE UNIQUE INDEX ... ON user_addresses (user_id) WHERE is_default`.

**Consequences.** "At most one default address per user" is a database invariant,
not a convention that concurrent writes could violate.

---

### ADR-014 — Frontend calls the API directly; no Vite dev proxy
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** The client calls `VITE_API_BASE_URL` directly, exercising the real
CORS middleware in development.

**Consequences.** CORS misconfiguration surfaces on day one instead of at deploy
time. Costs one more thing to configure correctly locally.

---

### ADR-015 — Addresses: schema in Step 1, endpoints and UI in Step 2
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** Step 1 ships the `user_addresses` table, model, and repository. CRUD
endpoints and the profile UI for add/edit/set-default come in Step 2.

**Consequences.** Keeps Step 1 focused on authentication while the schema settles
early, so Step 2 is additive rather than a migration exercise.

---

### ADR-016 — One commit per phase; push only when asked
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** The original plan had a single scaffolding commit at the end.

**Decision.** Commit at each phase boundary, with `progress.md` updated in the same
commit. Never push without an explicit request.

**Consequences.** The history reads as a record of how the project was assembled,
which suits a learning project better than one opaque commit. Each commit is also
a rollback point. Costs a little ceremony per phase.

---

### ADR-017 — Structured logging with `log/slog`, request logs bridged into it
**Date:** 2026-08-15 · **Status:** Accepted

**Decision.** The standard library's `log/slog` is the only logger. Echo's request
logger is wired through `RequestLoggerWithConfig` into the same handler, so
request and application lines share one format and destination. Text output in
development, JSON elsewhere. Request status drives log level: 5xx logs at error,
4xx at warn, everything else at info.

**Consequences.** No third-party logging dependency. `HandleError: true` is
required on the request logger so the central error handler runs first and the
logged status matches what was actually sent.

---

### ADR-018 — Configuration is validated once at startup, reporting every problem
**Date:** 2026-08-15 · **Status:** Accepted

**Context.** Configuration read lazily fails at first use, often deep in a request
and far from the cause.

**Decision.** `config.Load` resolves and validates everything at boot and returns
a single error listing *all* problems rather than the first one. Environment
variables always beat `.env` files, so a shell override wins over a stale file.
Production refuses to start on the placeholder `JWT_SECRET` or a secret under 32
characters.

**Consequences.** One run surfaces every misconfiguration instead of a fix-rerun
loop. Cross-field rules live here too — for instance, a refresh TTL shorter than
the access TTL is rejected, since such a token could never renew anything.

---

### ADR-019 — Migrations embedded in the binary, applied on boot only in development
**Date:** 2026-08-19 · **Status:** Accepted — extends ADR-004

**Context.** ADR-004 settled on golang-migrate. Two questions were left open: where
the SQL lives at runtime, and when it runs.

**Decision.** The `.sql` files are embedded with `go:embed` in a `migrations`
package, and both the server and `cmd/migrate` read them from there. Migrations
run at startup when `DB_AUTO_MIGRATE` is true, which is the default in development
and only there; elsewhere the default is false and migrating is a deploy step run
against the standalone command.

**Consequences.** Neither binary depends on the working directory or on the SQL
files being deployed alongside it. In development, pulling a new migration and
running the server is enough. In production, schema changes stay a decision a
human makes at a known moment rather than a side effect of a rolling restart,
where several instances would otherwise race — golang-migrate's advisory lock
makes that safe, but "safe" is not the same as "intended".

**Note.** golang-migrate is driven through `database/sql` over pgx's `stdlib`
adapter rather than its URL constructor, which would have pulled `lib/pq` in as a
second Postgres driver.

---

### ADR-020 — UUID primary keys, generated by the database
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** `users.id` is returned by `/users/me` and will appear in URLs as the
API grows. A `bigserial` would make every user id guessable from any other.

**Decision.** `uuid` primary keys defaulting to `gen_random_uuid()`, which
PostgreSQL 13+ provides without an extension.

**Consequences.** No enumeration of users or orders from a single leaked id, and
ids can be generated client-side later without coordination. Costs 16 bytes per
key against 8, and random v4 values scatter index inserts rather than appending —
irrelevant at this scale, worth revisiting for a high-volume table.

---

### ADR-021 — `updated_at` maintained by a trigger, not by application code
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** With hand-written SQL (ADR-003), every UPDATE has to remember to set
`updated_at`. The one that forgets is silent — the column simply goes stale.

**Decision.** A shared `set_updated_at()` trigger function, attached to every
table carrying the column.

**Consequences.** The timestamp is correct regardless of which code path wrote the
row. The cost is that an UPDATE cannot set `updated_at` explicitly — the trigger
overwrites it — which matters only for backfills, where the trigger can be
disabled for the duration.

---

### ADR-022 — Session token columns: `jti` as text, refresh hashes as `bytea`
**Date:** 2026-08-19 · **Status:** Accepted

**Decision.** `sessions.access_jti` is `text` with a unique index;
`refresh_hash` and `previous_refresh_hash` are `bytea` constrained to exactly 32
bytes.

**Consequences.** The `jti` stays an opaque application-chosen string, so Phase 3
can generate it from `crypto/rand` without adding a UUID dependency for a value
the database never interprets. The hashes are stored as the raw SHA-256 digest
rather than hex text: half the bytes, no encoding to agree on, and the length
constraint turns "something other than a digest was written here" into an error
at the point of the write.
