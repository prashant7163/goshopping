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
**Date:** 2026-08-15 · **Status:** Accepted — layer order superseded by ADR-031

**Context.** Tailwind's preflight and MUI's baseline both emit resets and fight
over specificity, producing overrides that are hard to diagnose after the fact.

**Decision.** Declare `@layer mui, theme, base, components, utilities;` and place
MUI's emitted styles in the `mui` layer, configured at root mount. Exact API to be
verified against MUI v9's shipped types rather than assumed.

**Consequences.** Tailwind utilities reliably win over MUI defaults. Set up before
any UI is written, since retrofitting means re-auditing every component.

**Correction.** The order quoted above is wrong, and where it is declared matters
as much as what it says. Both are settled in ADR-031, which is the one to read.

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

---

### ADR-023 — `golang-jwt/jwt/v5` for access tokens, with the algorithm pinned
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** ADR-008 settled on HS256 JWTs as the access-token format but not on
an implementation. Hand-rolling one is a short piece of code and a long list of
ways to get verification subtly wrong.

**Decision.** `github.com/golang-jwt/jwt/v5`, parsed with `WithValidMethods`
restricted to HS256, `WithIssuer("goshopping")`, and `WithExpirationRequired`.

**Consequences.** Pinning the algorithm is the point of the choice: without it, a
token declaring `alg: none` or an asymmetric algorithm verified against the HMAC
secret is the classic JWT bypass. Requiring the issuer means a token minted by
some other service that happened to share the secret is still refused. `v5`
rather than `v4` because v5 is the maintained line and moved validation options
out of a callback, where they were easy to omit.

---

### ADR-024 — Signup signs the new account in
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** The Step 1 deliverables specify signup's status (`201`) and its
failure (`409`) but not its response body, while login's body is spelled out.

**Decision.** Signup creates a session and returns the same shape login does —
`{access_token, refresh_token, token_type, expires_in, user}` — with `201`.

**Consequences.** The frontend's signup thunk stores tokens and is done, instead
of chaining a second request with credentials it has to keep in memory to do so.
The cost is that signup writes a session row, so a stream of registrations that
never sign in leaves rows for the reaper. Reverting to a bare `{user}` body would
touch only the handler and the frontend thunk.

---

### ADR-025 — Services return sentinel errors; handlers choose status codes
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** Something has to decide that a wrong password is a `401` and a
duplicate email a `409`. Building `*apierror.Error` inside the service would be
fewer lines, at the price of putting HTTP in the layer that is supposed to be
independent of it.

**Decision.** `service` exports `ErrInvalidCredentials`, `ErrEmailTaken`,
`ErrInvalidToken`, `ErrUserNotFound`, and `ErrPasswordUnchanged`. One `translate`
function in `handler` maps them, and anything unrecognized becomes a `500`.

**Consequences.** The service stays callable from a future CLI or worker without
dragging status codes along, and the mapping is one readable switch rather than a
decision scattered across handlers. `default: Internal(err)` means a new sentinel
that nobody adds a case for surfaces as a `500` — noisy, but noisy in the logs
rather than silently mislabelled to the client.

---

### ADR-026 — A failed login costs the same whether or not the account exists
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** The deliverables require unknown-email and wrong-password to return
an identical message. That closes the obvious oracle and leaves a subtler one: a
wrong password runs bcrypt at cost 12 and takes a few hundred milliseconds, while
an unknown address returns as soon as the `SELECT` misses. The response time
answers the question the message refuses to.

**Decision.** When no user matches, compare the submitted password against a
decoy hash — generated once from random bytes, on the first miss, at the
configured cost — and discard the result.

**Consequences.** Both failures cost one bcrypt comparison. This equalizes the
dominant term, not every nanosecond; a determined attacker with clean timings
might still find a signal, and the real defence against enumeration at volume is
rate limiting, which Step 1 does not have. The decoy is built lazily so startup
is not delayed by a hash that a healthy deployment may never need.

---

### ADR-027 — Rotation is a compare-and-swap, and slides the session's expiry
**Date:** 2026-08-19 · **Status:** Accepted · extends ADR-008 and ADR-009

**Context.** Refresh reads the session by refresh-token hash and then writes the
new pair. Two requests carrying the same token can both complete the read before
either writes.

**Decision.** The rotating `UPDATE` carries `WHERE id = $1 AND refresh_hash = $2
AND revoked_at IS NULL AND expires_at > now()`, and treats "no row updated" as a
spent token. The new `expires_at` is `now() + JWT_REFRESH_TTL`, so an actively
used session does not expire 30 days after it began.

**Consequences.** Exactly one of a set of concurrent refreshes succeeds — verified
by firing six at once and seeing one `200` and five `401`. The losers are
indistinguishable from a replay and revoke every session for the user, which is
precisely the failure ADR-012's single-flight client exists to prevent; the race
being real, and its punishment total, is the argument for building single-flight
before the first parallel request rather than after.

Sliding the expiry means a session ends 30 days after its last use rather than
30 days after login. There is no absolute cap on session lifetime; adding one
would mean a second column, and is the kind of policy worth having a reason for
beyond symmetry.

---

### ADR-028 — The reaper runs in-process, sweeping at startup and on a ticker
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** Expired sessions are already inert — `IsActive` rejects them — so
deleting them is housekeeping, not correctness. It still has to happen, or the
table the middleware searches on every request grows forever.

**Decision.** A goroutine started by the server, sweeping once immediately and
then every `SESSION_REAP_INTERVAL`, deleting rows that expired or were revoked
more than `SESSION_REAP_GRACE` ago. It stops before the connection pool closes.

**Consequences.** No cron entry, no second deployable. With several instances
they all sweep, which is harmless: the `DELETE` is idempotent and the losers
simply remove nothing. Sweeping at startup rather than only after the first tick
matters because the interval (24h) is longer than the interval between restarts
in development, so the delayed form would effectively never run.

The grace period keeps recently dead sessions readable, which is the difference
between diagnosing "this expired yesterday" and finding no row at all. Revoked
sessions are swept by revocation time, not expiry, or a session revoked on day
one would sit until day thirty.

---

### ADR-029 — Moving the default address needs a transaction, and the repository insists
**Date:** 2026-08-19 · **Status:** Accepted · extends ADR-013

**Context.** ADR-013 made "at most one default address per user" a partial unique
index. Moving the default therefore means clearing one row and setting another,
and the obvious single statement —

```sql
UPDATE user_addresses SET is_default = (id = $1) WHERE user_id = $2 AND (id = $1 OR is_default)
```

— succeeds or fails depending on which row the scan reaches first. Tried both
ways against PostgreSQL 17: clearing-then-setting worked, and the same statement
in the other direction failed with a duplicate key on the index. The index is
checked as each row is written, and a partial unique *index* cannot be made
deferrable the way a unique *constraint* can.

**Decision.** `SetDefault` clears first and then sets, as two statements, and
returns an error unless it is running inside `Store.Tx`. The `Store` a
transaction hands to its callback knows it is transactional; the one built over
the pool does not.

**Consequences.** A caller who forgets the transaction gets a loud error instead
of an interruption that could leave a user with no default address at all. The
check costs a boolean field on the repository. Ordinary reads and writes are
unaffected — only this one operation spans two rows.

---

### ADR-030 — Repository tests run against a real PostgreSQL, and skip without one
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** The address repository ships in Step 1 with no endpoints in front of
it until Step 2 (ADR-015), so nothing would exercise it. What most needs
exercising is behaviour that belongs to PostgreSQL rather than to Go: the partial
unique index, the `updated_at` trigger, `ON DELETE CASCADE`, and the row-order
sensitivity in ADR-029. A mock would assert only that the code sends the SQL it
sends.

**Decision.** `internal/repository` has integration tests that connect to the
development database, create throwaway accounts, and remove them afterwards. With
no database reachable they call `t.Skip`, so `go test ./...` still passes on a
machine that has not started Docker.

**Consequences.** The tests verify the invariants the schema actually enforces.
They need a running database to mean anything, and skipping quietly means a
misconfigured CI could report success while testing nothing — so any CI added
later must fail if these skip. They also share one database rather than a fresh
one per run, which is why each test creates its own user and scopes everything to
it.

---

### ADR-031 — MUI's layer sits between Tailwind's reset and its utilities, declared in `index.html`
**Date:** 2026-08-19 · **Status:** Accepted — supersedes the layer order in ADR-005

**Context.** ADR-005 fixed the mechanism — cascade layers — and quoted the order
from the original brief: `@layer mui, theme, base, components, utilities;`, with
the note that the API should be verified rather than assumed. Two things about
that turned out to be wrong, and both fail silently.

**The order.** Ranking `mui` first makes it the *weakest* layer, so Tailwind's
preflight beats MUI's own component styles. Preflight resets `button` to a
transparent box with no padding, so every MUI Button renders flat. What made this
worth catching properly is that the obvious test still passes: "does a Tailwind
background beat the MUI background" is trivially true when MUI no longer has one.
Measured in Chrome, a contained Button came out `rgba(0, 0, 0, 0)` with `0px`
padding.

**The placement.** Declaring the order in `index.css` is too late. MUI inserts
its `CssBaseline` styles at the very top of `<head>` at runtime — before any
stylesheet Vite injects — so `mui` is already established as the first layer by
the time the declaration is parsed. The cascade ranks layers by where their names
*first appear*, and a later statement cannot reorder them.

**Decision.** The statement is `@layer theme, base, mui, components, utilities;`,
inline in `index.html`, immediately followed by
`<meta name="emotion-insertion-point" content="" />`. MUI reuses that tag rather
than prepending its own, which is what keeps its global styles below the
declaration instead of above it. `<StyledEngineProvider enableCssLayer>` at the
root is what puts MUI's rules in the `mui` layer at all — verified against MUI
v9.3.1's shipped source, which wraps each Emotion rule in `@layer mui { … }` and
gives that cache its own key.

**Consequences.** MUI components keep their own styling, and Tailwind utilities
still override them with no `!important` and no specificity games. Resulting
order in the browser: `theme < base < mui < components < utilities`. Two files
now have to agree, which is why `index.html` carries the reasoning and
`src/index.css` points at it.

The check lives in the app itself (`src/App.tsx`), asserting against computed
styles in a real browser, because Emotion injects at runtime and none of this is
visible in the built stylesheet. It includes the case that would otherwise pass
by accident: that MUI's own button styling survived the reset.

---

### ADR-032 — MUI v9 drops the system-prop shorthands
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** `<Typography fontWeight={700}>` and `<Stack alignItems="center">`
are how MUI v5 and v6 were usually written. In v9 those props no longer exist,
and the compiler says so through a long overload-resolution error that does not
obviously mean "this prop was removed".

**Decision.** Use Tailwind utility classes for that kind of styling — `font-bold`,
`items-center` — and reserve `sx` for values that have to come from the theme.

**Consequences.** One way to express spacing and weight rather than two
overlapping ones, and it exercises the layering from ADR-031 on every page. The
cost is that MUI examples found online will often need translating.

---

### ADR-033 — CI runs on GitHub Actions, against a real database and a real browser
**Date:** 2026-08-19 · **Status:** Accepted

**Context.** Two of this project's rules cannot be checked by reading code, and
both fail quietly. The cascade layer order (ADR-031) leaves the app building,
type-checking, and linting clean while every MUI component renders flat. The
repository tests (ADR-030) pass by skipping when no database is reachable.

**Decision.** One workflow, two jobs. The backend job runs against a Postgres
service container with `REQUIRE_DATABASE` set, so a skipped integration test is a
failed build rather than a green one; it applies migrations, runs `gofmt`,
`go vet`, the tests, and then `scripts/smoke_auth.sh` against a real server. The
frontend job type-checks, lints, and runs the Playwright suite in Chromium
against both the dev server and the production build.

**Consequences.** Both silent failure modes are now loud. The layer suite was
confirmed to fail before it was trusted: reverting to ADR-005's order failed two
checks, and dropping `enableCssLayer` failed two different ones — while three
checks still passed in each case, which is the argument for asserting that MUI's
own styling survived the reset rather than only that Tailwind overrides
something.

Chromium alone, because what is being measured is how the cascade resolves
layers rather than how an engine paints. Both servers, because Vite injects CSS
as `<style>` elements in development and a `<link>` in the build, and the
ordering bug turned on exactly that difference.

The cost is a Playwright dependency and a browser download per run, and the
`JWT_SECRET` the workflow sets so `config.Load` (ADR-018) will start at all — not
a secret, since nothing in CI signs a token anybody will present.
