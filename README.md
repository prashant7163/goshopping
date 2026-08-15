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

# 3. Backend (from ./backend, once Phase 1 lands)
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
