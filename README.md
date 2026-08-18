# GoShopping

A full-stack e-commerce project built to practice Go and React with TypeScript.

- **Backend** — Go + Echo, PostgreSQL via pgx, JWT auth with server-side sessions
- **Frontend** — React 19 + Vite + React Router, Redux Toolkit, MUI + Tailwind v4

| Document | Purpose |
| --- | --- |
| [project-setup.md](project-setup.md) | Requirements and scope |
| [progress.md](progress.md) | What is built, what is next |
| [decisions.md](decisions.md) | Why things are built the way they are |

## Prerequisites

Go 1.26+, Node 22+, Docker with Compose v2+, Git.

## Quick start

```bash
# 1. Environment files
cp .env.example .env
cp backend/.env.example backend/.env
cp frontend/.env.example frontend/.env

# 2. Database
docker compose up -d
docker compose ps          # wait for postgres to report "healthy"

# 3. Backend (from ./backend) — migrates the schema on boot in development
go run ./cmd/server

# 4. Frontend (from ./frontend, once Phase 4 lands)
npm install && npm run dev
```

## Ports

| Service | Port |
| --- | --- |
| Postgres | 5432 |
| Backend API | 8080 |
| Frontend dev server | 5173 |

## Layout

```
.
├── backend/            Go API (Echo, pgx, golang-migrate)
├── frontend/           React SPA (Vite, Redux Toolkit, MUI + Tailwind)
├── docker-compose.yml  PostgreSQL 17
└── .env.example        Compose credentials
```

## Environment notes

Each tier has its own `.env`, all git-ignored; the `.env.example` files are the
tracked templates. Two things to watch:

- `DATABASE_URL` in `backend/.env` must match the credentials in the root `.env`,
  since Compose reads the root file.
- Only `VITE_`-prefixed variables reach the browser, and they are embedded in the
  shipped bundle. Never put a secret in `frontend/.env`.

The default `JWT_SECRET` is a development placeholder. Generate a real one for any
deployed environment:

```bash
openssl rand -base64 48
```

## Useful commands

```bash
docker compose logs -f postgres    # tail database logs
docker compose down                # stop, keeping data
docker compose down -v             # stop and delete the volume (destroys data)

# psql shell
docker compose exec postgres psql -U goshopping -d goshopping
```

## Migrations

The server applies pending migrations at startup while `DB_AUTO_MIGRATE` is true,
which it is by default in development. Set it to false and migrate deliberately
with the standalone command (from `./backend`):

```bash
go run ./cmd/migrate up          # apply every pending migration
go run ./cmd/migrate down        # roll back one
go run ./cmd/migrate down all    # roll back everything
go run ./cmd/migrate version     # print the applied version
go run ./cmd/migrate force 3     # clear the dirty flag after a hand-fixed failure
```

Migration files live in [backend/migrations/](backend/migrations/) as numbered
`.up.sql`/`.down.sql` pairs and are embedded in the binary, so neither command
depends on the working directory.

## API

Every response, success or failure, is JSON. Errors share one envelope:

```json
{"error": {"code": "validation_failed", "message": "one or more fields are invalid",
           "details": {"password": "password must be at least 8 characters"}}}
```

| Method | Path | Auth | Result |
| --- | --- | --- | --- |
| `GET` | `/healthz` | — | `200` with database status, `503` when it is down |
| `POST` | `/api/v1/auth/signup` | — | `201` token pair, `409` if the email is taken |
| `POST` | `/api/v1/auth/login` | — | `200` token pair, `401` on bad credentials |
| `POST` | `/api/v1/auth/refresh` | refresh token in body | `200` new pair, `401` if spent |
| `POST` | `/api/v1/auth/logout` | Bearer | `204`, revoking that session only |
| `GET` | `/api/v1/users/me` | Bearer | `200` profile |
| `PATCH` | `/api/v1/users/me/password` | Bearer | `200` new pair, other devices signed out |

Authenticated requests carry `Authorization: Bearer <access_token>`. The access
token is a 24-hour HS256 JWT; the refresh token is an opaque 30-day string that
is rotated on every use. Both are checked against a `sessions` row, which is what
makes a logout or a password change take effect immediately rather than whenever
the token happens to expire — see ADR-007 in [decisions.md](decisions.md).

Presenting a refresh token that has already been rotated past is treated as
theft: every session for that user is revoked. A client must therefore never run
two refreshes at once (ADR-012).

## Verifying the auth flow

With the server running and `jq` installed:

```bash
cd backend && ./scripts/smoke_auth.sh          # or: ./scripts/smoke_auth.sh http://host:port
```

It signs up, logs in, refreshes, changes a password, and logs out across two
throwaway accounts, asserting the rules that no single response shows — that both
login failures are identical, that a password change keeps the caller signed in
while ending every other session, and that a replayed refresh token revokes
everything.

The Go tests cover what the schema enforces rather than what the code says —
the one-default-address index, the `updated_at` trigger, the cascade — so they
talk to the database in `DATABASE_URL`, creating and removing their own accounts:

```bash
cd backend && go test ./...
```

With no database reachable they skip rather than fail, so the suite still passes
before `docker compose up`.
