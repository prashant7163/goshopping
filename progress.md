# Progress

**Last updated:** 2026-08-15
**Current status:** Phase 1 complete — Echo server runs, `/healthz` reports database state.
**Next up:** Phase 2 — migrations for `users`, `user_addresses`, and `sessions`.
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

### Phase 2 — Schema & migrations ⬜
- [ ] golang-migrate wired in, runs on boot in development
- [ ] `000001_create_users` — `citext` email, bcrypt hash, timestamps
- [ ] `000002_create_user_addresses` — FK cascade, partial unique index on default
- [ ] `000003_create_sessions` — `access_jti`, `refresh_hash`, `previous_refresh_hash`, `revoked_at`
- [ ] Down migrations verified to reverse cleanly

### Phase 3 — Auth & user endpoints ⬜
- [ ] bcrypt hashing, password policy (min 8 chars, max 72 bytes)
- [ ] Session service: issue, validate, rotate, revoke, revoke-all, reap
- [ ] JWT middleware validating signature *and* session row
- [ ] `POST /api/v1/auth/signup`
- [ ] `POST /api/v1/auth/login`
- [ ] `POST /api/v1/auth/refresh` — rotation with reuse detection
- [ ] `POST /api/v1/auth/logout`
- [ ] `GET /api/v1/users/me`
- [ ] `PATCH /api/v1/users/me/password` — revokes all sessions, returns new pair
- [ ] Uniform error envelope; no password hash in any response
- [ ] Background session reaper on a ticker, stopped on shutdown

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
