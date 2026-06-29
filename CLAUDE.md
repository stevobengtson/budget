# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Local-first personal finance app with envelope budgeting. Two-workspace monorepo:
- `api/` — Go backend: JSON API (multi-user, JWT auth) + TUI (single-user, Bubble Tea)
- `web/` — TanStack Start (React) frontend with BetterAuth, Drizzle ORM, Shadcn UI

Both share the same Postgres database. The web app handles auth and serves as the primary UI going forward; the TUI remains for local/offline use with SQLite. The TUI uses SQLite, the multi-user API uses Postgres — the store layer abstracts both dialects.

## Build & Run

### API (Go) — all commands run from `api/`

```bash
make setup          # go mod download + install goose CLI
make build          # compile both binaries to api/bin/{tui,api}/budget
make test           # go test ./...
make run            # build + launch TUI (alias: make tui)
make api            # build + launch JSON API server
make dev            # hot-reload API via air (uses .air.toml)

# Single test file/function:
go test ./internal/core/store/ -run TestAccountCRUD -v

# Database (SQLite — TUI local dev):
make db-migrate     # goose up (SQLite)
make db-reset       # delete SQLite + re-migrate
make db-status      # goose migration status
make seed           # migrate + load demo data

# Database (Postgres — API multi-user):
make migrate-api              # goose up against API_DSN
make migrate-api-status       # goose migration status for Postgres
# Override DSN: make migrate-api API_DSN='postgres://...'
```

Go 1.26 module: `github.com/sbengtson/budget`.

### Web (TanStack Start) — all commands run from `web/`

```bash
pnpm install
pnpm dev            # vite dev server on port 3005
pnpm build          # production build
pnpm test           # vitest run
pnpm check          # biome check (lint + format) — run before done
pnpm lint           # biome lint only
pnpm format         # biome format only

# Database (Postgres via Drizzle):
pnpm db:push        # ensure-db.ts then drizzle-kit push (requires DATABASE_URL)
pnpm db:generate    # generate migration files
pnpm db:migrate     # ensure-db.ts then run migrations
pnpm db:studio      # open Drizzle Studio
```

`db:push`/`db:migrate` run `scripts/ensure-db.ts` first to create the DB if missing.

### CI

GitHub Actions runs `go test ./...` and builds both Go binaries (`cmd/tui`, `cmd/api`) from `api/`.

## Architecture

### API (`api/`)

```
cmd/tui/main.go          TUI binary — single-user, SQLite, Bubble Tea
cmd/api/main.go          API binary — multi-user, JWT auth, Gin HTTP server
internal/cli/            Shared Cobra commands (root flags, config, db, migrate, seed)
internal/core/store/     Persistence layer — raw SQL with dialect abstraction (SQLite/Postgres)
internal/core/db/        DB open + embedded goose migrations (separate dirs for sqlite/postgres)
internal/core/config/    Viper-based config (budget.yaml / BUDGET_* env / CLI flags)
internal/core/money/     Cents ↔ human string parsing
internal/core/format/    Display formatting helpers
internal/core/paydown/   Debt amortization projection (pure Go, no DB)
internal/core/settings/  User-level settings store
internal/api/            Gin router setup, NewServer()
internal/api/handlers/   JSON handlers — all scoped via store.For(userID)
internal/api/middleware/ JWT verification against BetterAuth JWKS (EdDSA)
internal/tui/            Bubble Tea screens and components
```

**Key patterns:**
- `store.Store` is the base persistence layer; `store.UserStore` (via `store.For(userID)`) scopes all queries to a single user — handlers always use UserStore
- `LocalUserID` sentinel (`00000000-...0001`) owns TUI-created rows
- SQL uses `?` placeholders; dialect helper rebinds for Postgres (`$1`, `$2`, ...)
- Migrations live in `internal/core/db/migrations/{sqlite,postgres}/` — keep both in sync
- Amounts stored as integer cents throughout; `money` package handles formatting
- Config precedence: CLI flag → `BUDGET_*` env → budget.yaml → defaults

### Web (`web/`)

```
src/routes/              TanStack Router file-based routes
  __root.tsx             Root layout (providers, devtools)
  _authed.tsx            Auth-guarded layout
  _authed/budget.tsx     Budget page
  _authed/accounts.$accountId.tsx   Account detail
  _authed/transactions.tsx
  _public/               Public routes (login, landing)
  api/auth/$.ts          BetterAuth API catch-all route
src/server/              Backend-for-frontend (BFF) — server fns that bridge to the Go API
  go-api.ts              Mints a BetterAuth JWT per session, forwards as Bearer to Go API
  accounts.ts            createServerFn wrappers calling goApiFetch
  auth-helpers.ts        Server-side auth helpers
src/features/            Feature modules (accounts, budget, transactions)
  */queries.ts           TanStack Query options with query key factories
src/db-collections/      TanStack DB (react-db) local-only collections
src/components/ui/        Shadcn UI components (radix-ui based)
src/components/auth/      Auth/settings/passkey UI
src/db/schema/           Drizzle ORM schema (auth-schema.ts)
src/lib/api/types.ts     Shared TS types mirroring API DTOs
src/lib/fake/            In-memory fake data layer (offline/prototype dev)
src/hooks/               React hooks (e.g. use-mobile)
src/paraglide/           i18n (Paraglide)
```

**Key patterns:**
- TanStack Start (SSR-capable React framework on Vite)
- File-based routing: `_authed` layout requires auth, `_public` is open
- Path aliases: both `#/*` and `@/*` resolve to `./src/*` (both appear in imports)
- Feature modules export query factories using `queryOptions()` from `@tanstack/react-query`
- Shadcn components via `radix-ui` + `class-variance-authority` + `tailwind-merge`
- Auth: BetterAuth with passkey support; JWT issued to Go API
- Styling: Tailwind CSS v4, Catppuccin Mocha theme
- Biome for linting/formatting (tab indent, double quotes)

**Web ↔ Go API data flow (the BFF bridge):**
The browser only ever talks to the TanStack Start server via cookie session — it never calls the Go API directly. Server functions in `src/server/` call `goApiFetch` (`go-api.ts`), which mints a short-lived BetterAuth JWT for the current session (from the `set-auth-jwt` header of `getSession`) and forwards it as a Bearer token to the Go API (`GO_API_URL`, default `http://localhost:8080`). The Go API verifies the JWT against BetterAuth's JWKS — no shared session store. Go DTOs are camelCase with cents as integers.

**Migration in progress (frontend → real API):**
The frontend is mid-migration from the in-memory fake layer to the Go API. As of now only accounts has a real BFF server fn (`server/accounts.ts`); `features/{accounts,budget,transactions}/queries.ts` still import from `lib/fake/db.ts`. When wiring a feature to the real API, add a server fn under `src/server/` using `goApiFetch` and repoint that feature's `queries.ts` off `lib/fake`. `db-collections/index.ts` is currently a demo `messages` collection, not real domain data.

## Key Rules

- NEVER commit API keys or secrets — use environment variables
- Run `go test ./...` in `api/` before considering backend work done
- Run `pnpm check` in `web/` before considering frontend work done
- Do not commit, suggest commits, or create PRs unless explicitly asked
- Amounts are always integer cents in code; display formatting happens at the boundary
- Keep SQLite and Postgres migration directories in sync when adding schema changes
