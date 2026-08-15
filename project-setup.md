You are a principal software engineer helping me build a full-stack e-commerce project (GoShopping) to practice Go and ReactJS with TypeScript.

### Project Setup & Version Control
- **Root Directory Structure:** Create two main subdirectories under `GoServe/GoShopping`:
  - `GoServe/GoShopping/backend`
  - `GoServe/GoShopping/frontend`
- **Git & GitHub Remote:** 
  - Initialize a Git repository at `GoServe/GoShopping`.
  - Include `.gitignore` rules for Go, Node.js, Vite, `.env` files, and Docker volumes.
  - Set Git remote origin to `https://github.com/prashant7163/goshopping`.
- **Database & Environment Setup:** 
  - Spin up PostgreSQL using `docker-compose.yml` (Port: `5432`).
  - Pin default credentials: DB=`goshopping`, User=`goshopping`, Pass=`goshopping`.
  - Provide `.env.example` files in root, backend, and frontend with all default variables.
- **Backend Architecture:** Clean Go layout (`/cmd/server`, `/internal/handler`, `/internal/service`, `/internal/repository`, `/internal/model`).
- **Project Documentation:** Maintain two living documents at the project root:
  - `progress.md` — phase-by-phase build status, updated at every phase boundary.
  - `decisions.md` — numbered ADR log recording why each technical choice was made.
- **Commit Cadence:** One commit per completed phase. Push only on explicit request.

---

### Tech Stack Specifications

#### Backend (`/backend`)
- **Language & Framework:** Go with **Echo** framework.
- **Middlewares:** CORS (configured for frontend origin), JWT Auth middleware, Request Logging, `/healthz` health check endpoint.
- **Database Access:** PostgreSQL via `pgx` / standard SQL with migration support.
- **Authentication & Security:** 
  - Password hashing with `bcrypt`.
  - Password validation: Min 8 characters, max 72 bytes.
  - **Session-backed JWT auth — deliberately not stateless** (see ADR-007 in `decisions.md`). A `sessions` table is the source of truth: one row per login, holding the current access-token `jti` and a SHA-256 hash of the current refresh token. The JWT middleware verifies the signature, then validates the session row. This is what makes immediate revocation possible with long-lived tokens.
  - Access token: JWT HS256 carrying a random `jti`. TTL via `JWT_ACCESS_TTL`, default **24h**.
  - Refresh token: opaque 32 random bytes, stored only as a hash. TTL via `JWT_REFRESH_TTL`, default **30d**. Rotated in place on every refresh, keeping `sessions.id` stable.
  - Reuse detection: presenting an already-rotated refresh token revokes **every** session for that user.
  - Changing a password revokes all of the user's sessions and issues one fresh pair — every other device is signed out while the calling session continues uninterrupted.
  - Background reaper deletes expired session rows on a ticker.

#### Frontend (`/frontend`)
- **Framework & Build Tool:** **ReactJS (Vite)** with **TypeScript** and **React Router v6/v7**.
- **State Management:** Redux Toolkit with Thunk middleware. Hydrate both access and refresh tokens from `localStorage` before first render, through a single `tokenStorage` module (the one seam to change if tokens later move to httpOnly cookies).
- **API Client:** Central fetch wrapper that injects the `Authorization` header, normalizes the backend error envelope, and on `401` performs a **single-flight** refresh — concurrent callers await one shared promise — before replaying the original request. Without single-flight, parallel refreshes trip reuse detection and sign the user out. A failed refresh clears auth and redirects to `/login`.
- **UI & Styling Configuration (MUI + Tailwind v4 Integration):**
  - Use Material UI (MUI) combined with TailwindCSS v4.
  - **CSS Specificity / Reset Fix:** To prevent style conflicts between Tailwind v4 resets and MUI baseline styles:
    - Set up CSS Cascade Layers in `index.css`: `@layer mui, theme, base, components, utilities;`
    - Configure MUI's `EngineProvider` or theme settings with `enableCssLayer: true` (or standard `StyledEngineProvider` with `injectFirst`) on root mount to guarantee correct cascade order from day one.

---

### Step 1 Deliverables (Baseline Authentication & Shell)

#### 1. Backend REST Endpoints
Implement the following REST routes with proper status codes and error JSON responses:
1. `GET /healthz` → `200`: Basic system health check, including a DB ping.
2. `POST /api/v1/auth/signup` → `201`: Create a user (email, password, full name). `409` if the email is taken.
3. `POST /api/v1/auth/login` → `200`: Validate credentials, create a session, return `{access_token, refresh_token, expires_in, user}`. Unknown email and wrong password must return an identical message, so the endpoint does not leak which accounts exist.
4. `POST /api/v1/auth/refresh` → `200`: Rotate the token pair. Presenting an already-rotated token revokes every session for that user.
5. `POST /api/v1/auth/logout` → `204`: Revoke the current session only.
6. `GET /api/v1/users/me` → `200`: Return authenticated profile details (JWT protected).
7. `PATCH /api/v1/users/me/password` → `200`: Verify the current password, update it, revoke **all** the user's sessions, then return a fresh token pair so the calling session stays signed in.

All errors share one envelope: `{"error": {"code", "message", "details"}}`. No response ever includes the password hash.

#### 2. Data Model (Step 1)
- `users` — `citext` email (unique), bcrypt hash, full name, timestamps.
- `sessions` — one row per login: `access_jti`, `refresh_hash`, `previous_refresh_hash`, `revoked_at`, `expires_at`, `user_agent`.
- `user_addresses` — schema only in Step 1; endpoints and UI land in Step 2.

#### 3. Frontend Views & Flows
1. **Landing Page (`/`)**:
   - Header with logo, navigation, and conditional Auth state:
     - Logged out: Displays **Login** and **Sign Up** buttons.
     - Logged in: Displays user name, **Profile** link, and a **Logout** button. Logout must call `POST /api/v1/auth/logout` to revoke the session server-side before clearing Redux and localStorage — clearing the client alone leaves the session row valid.
   - Clean footer and Hero banner.
2. **Sign Up (`/signup`) & Login (`/login`) Pages**:
   - Forms with field validation matching backend rules (min 8 chars password).
   - Redux Thunk integration with backend auth endpoints.
3. **User Profile (`/profile`)**:
   - Protected route showing current user profile details.
   - Form for updating user password calling `PATCH /api/v1/users/me/password`, storing the returned token pair so the session survives the change.

---

### Step 2 Preview: User Addresses
*The `user_addresses` table ships in Step 1 (see Data Model above); Step 2 exposes it.*
- **Model:** `UserAddress` — one user has many addresses: street, city, state, postal code, country, `is_default`. At most one default per user, enforced by a partial unique index rather than by application code.
- **Endpoints (Step 2):** list, create, update, delete, set-default.
- **Profile UI (Step 2):** view, add, edit, and set default addresses on the `/profile` page.
- **Active sessions view (Step 2):** made possible by the `sessions` table — list devices, revoke individually.

---

### Execution Instructions
0. Track every phase in `progress.md` and every non-obvious choice in `decisions.md`. Commit at each phase boundary; do not push without being asked.
1. Scaffold workspace directories, initialize Git (`https://github.com/prashant7163/goshopping`), and create `docker-compose.yml` + `.env.example`. ✅ **done**
2. Initialize the backend Go module (`go mod init`) with Echo framework and database migration scripts.
3. Initialize React TypeScript frontend using Vite, setup Tailwind v4 and MUI with the CSS Cascade Layer config, and add `@reduxjs/toolkit` and `react-router-dom`.
4. Implement models, migrations, CORS middleware, JWT auth, and handlers.
5. Build Redux slices/thunks, local storage hydration, header auth state toggle, and UI routes.
6. Verify end-to-end user signup, login, token refresh, profile fetch, password change, and logout flows — including that a password change kills other sessions while the current one survives.