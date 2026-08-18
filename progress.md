# Progress

**Last updated:** 2026-08-19
**Current status:** Phase 3 complete — the whole authentication surface is implemented and verified against a running server.
**Next up:** Phase 4 — frontend scaffold.
**Blockers:** None.

Scope and requirements live in [project-setup.md](project-setup.md); the reasoning
behind technical choices lives in [decisions.md](decisions.md).

One commit per completed phase, this file updated in the same commit. Pushing
happens only on request — see ADR-016.

---

## Step 1 — Baseline Authentication & Shell

### Phase 0 — Workspace, Git, Docker ✅
- [x] `backend/` and `frontend/` directories
- [x] Git initialized on `main`, remote origin set to `prashant7163/goshopping` (not pushed)
- [x] `.gitignore` for Go, Node, Vite, env files, Docker volumes, macOS
- [x] `docker-compose.yml` — Postgres 17-alpine, named volume, `pg_isready` healthcheck
- [x] `.env.example` at root, `backend/`, and `frontend/`
- [x] `progress.md` and `decisions.md`
- [x] `README.md`
- [x] Postgres verified reachable

### Phase 1 — Backend skeleton ✅
- [x] `go mod init github.com/prashant7163/goshopping/backend`; Echo v4.15.4, pgx v5.10.0, godotenv v1.5.1
- [x] Packages: `cmd/server`, `internal/{config,db,apierror,middleware,handler}`
      (`model`, `repository`, `service` land in Phases 2–3, when they have content)
- [x] Typed config from env, aggregating every validation problem into one failure
- [x] pgx pool, verified at startup rather than on first query
- [x] Echo server: request ID, slog request logging, recovery, CORS, graceful shutdown
- [x] Central error envelope via Echo's `HTTPErrorHandler`
- [x] `GET /healthz` reporting DB status
- [x] Verified: `go vet` and `gofmt` clean; health 200/503; error envelope on 404/405;
      CORS allows `:5173` and refuses others; config and DB failures exit non-zero;
      pool recovers from a database restart without restarting the server

### Phase 2 — Schema & migrations ✅
- [x] golang-migrate wired in over pgx's `stdlib` adapter, no second driver
- [x] Migrations embedded with `go:embed`, so neither binary depends on the working directory
- [x] Runs on boot while `DB_AUTO_MIGRATE` is true — the default in development only (ADR-019)
- [x] `cmd/migrate` for the deliberate path: `up`, `down [n|all]`, `version`, `force`
- [x] `000001_create_users` — `citext` email, bcrypt-shaped hash, uuid key, `updated_at` trigger
- [x] `000002_create_user_addresses` — FK cascade, partial unique index on default
- [x] `000003_create_sessions` — `access_jti`, `refresh_hash`, `previous_refresh_hash`,
      `revoked_at`, `expires_at`, `user_agent`, indexed for the middleware and the reaper
- [x] Verified: citext rejects a differently-cased duplicate; a plaintext password fails the
      bcrypt check; a second default address is refused; a non-32-byte refresh hash is refused;
      rotation in place keeps `sessions.id` stable; deleting a user cascades to both children;
      `updated_at` bumps on update; `down all` leaves no tables, no `citext`, no trigger function;
      three concurrent `up` runs serialize on the advisory lock; a dirty database refuses to
      migrate until forced

### Phase 3 — Auth & user endpoints ✅
- [x] Packages filled in: `model`, `repository`, `service`, plus `security` for
      the pieces that are neither storage nor policy (hashing, token formats)
- [x] bcrypt hashing, password policy (min 8 characters, max 72 bytes)
- [x] Session service: issue, validate, rotate, revoke, revoke-all, reap
- [x] JWT middleware validating signature *and* session row, loading the user in
      the same query so `/users/me` costs no second round trip
- [x] `POST /api/v1/auth/signup` — 201 with a token pair (ADR-024), 409 on a taken email
- [x] `POST /api/v1/auth/login`
- [x] `POST /api/v1/auth/refresh` — compare-and-swap rotation with reuse detection
- [x] `POST /api/v1/auth/logout`
- [x] `GET /api/v1/users/me`
- [x] `PATCH /api/v1/users/me/password` — revokes all sessions, returns new pair
- [x] Uniform error envelope; `password_hash` is `json:"-"` on the model, so no
      response can carry it
- [x] Background session reaper on a ticker, sweeping at startup and stopped
      before the pool closes
- [x] `UserAddress` model and repository, completing ADR-015's Step 1 half —
      owner-scoped queries, and `SetDefault` refusing to run outside a transaction
- [x] `scripts/smoke_auth.sh` — 40 assertions over the whole surface; Phase 6
      extends it and records its output
- [x] `internal/repository` integration tests covering the address rules the
      schema enforces; they skip when no database is reachable
- [x] Verified against a running server, all 40 assertions passing: signup is 201
      and a differently-cased duplicate is 409; invalid signup reports all three
      fields at once; a 73-byte password is refused; unknown email and wrong
      password return byte-identical bodies; `/users/me` is 401 without a token,
      with a garbage token, and after logout; rotation issues a new pair and
      kills the previous access token; six concurrent refreshes of one token
      yield exactly one 200; a replayed refresh token revokes every session for
      that user and none for anyone else; a password change keeps the caller
      signed in, signs out the other device, and stops it refreshing; the old
      password no longer logs in; the reaper deletes rows past the grace window
      and keeps ones inside it; CORS still answers a PATCH preflight from the
      Vite origin and refuses another; `go vet`, `gofmt`, and `go test ./...` clean

### Phase 4 — Frontend scaffold ⬜
- [ ] Vite + React + TypeScript
- [ ] Tailwind v4 via `@tailwindcss/vite`
- [ ] MUI v9 + Emotion, cascade layers verified working
- [ ] Redux Toolkit, react-redux, react-router-dom

### Phase 5 — Redux, routing, pages ⬜
- [ ] `tokenStorage` seam + hydration before first render
- [ ] `apiClient` with auth header, error normalization, single-flight refresh
- [ ] `authSlice` + thunks: signup, login, fetchMe, changePassword, logout
- [ ] Header with conditional auth state; footer; hero
- [ ] `/`, `/signup`, `/login`, `/profile` behind `RequireAuth`
- [ ] Client-side validation mirroring backend rules

### Phase 6 — Verification ⬜
- [ ] Backend smoke script: signup → login → me → refresh → change password → re-login
- [ ] Old password rejected after change; other sessions confirmed dead
- [ ] Unauthenticated `/users/me` returns 401
- [ ] Browser walkthrough of all four routes plus logout
- [ ] Actual output recorded, not assumed

### Phase 7 — Final review & push ⬜
- [ ] Full read-through of the Step 1 diff
- [ ] Push to `origin/main` (only on explicit request — see ADR-016)

---

## Step 2 — Planned

- Address CRUD endpoints and profile UI (add / edit / set default) — see ADR-015
- Active-sessions view, enabled by the sessions table — see ADR-007
- Products, cart, checkout
