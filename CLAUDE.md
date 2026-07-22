# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## Project Overview

Personal finance app with envelope budgeting. **Single Go module**
(`github.com/sbengtson/budget`):

- **Web** (`cmd/web`) — server-rendered web UI (Gin + HTMX + Templ).

**Postgres is the only supported store** (via the pgx driver). All store SQL uses
native `$1,$2,...` placeholders — there is no SQLite backend or dialect/rebind layer.
No separate JS frontend — the web UI is Go templates + HTMX.

## Build & Run

All commands run from the repo root via [Task](https://taskfile.dev)
(`Taskfile.yml`). Run `task` with no args to list targets. (The old `Makefile`
was removed in favor of Task on 2026-07-12.)

**Devbox (preferred toolchain).** `devbox.json` pins the exact tool versions
(go, go-task, templ, goose, air, tailwindcss_4) — the single source of
truth, matched to `go.mod` + the Taskfile. Work inside `devbox shell` (or `devbox
run <script>`; scripts: `web`, `tui`, `dev`, `test`, `migrate`, `seed`). The
shell's `init_hook` exports `TAILWIND`/`TEMPL` to the nix binaries, so Task uses
them and **skips the tailwind download + `go install` steps**. `.air.toml` uses
`${TAILWIND}`/`${TEMPL}` with fallbacks so `task dev` works in and out of Devbox.
No CGO — the pgx driver is pure Go, so no compiler in the shell. A running
Postgres is required (app + tests). When adding a tool, `devbox add <pkg>` and pin it here.

```bash
task setup          # go mod download + install goose; installs templ + tailwind CLIs
task build          # build both binaries -> bin/tui/budget, bin/web/budget
task run            # build + launch web (alias: task web)
task web            # build + launch web server (regenerates css + templ first)
task dev            # hot-reload via air
task test           # templ generate + go test ./...

# Single test:
go test ./internal/core/store/ -run TestAccountCRUD -v

# Codegen (needed after editing .templ files or Tailwind sources):
task templ          # templ generate for internal/web
task css            # compile Tailwind -> internal/web/static/app.css
task tailwind-watch # watch mode

# Database (Postgres; DSN from BUDGET_DB_DSN, else the local budget default):
task db:migrate     # goose up (Postgres)
task db:reset       # goose reset + up (DESTRUCTIVE — wipes data)
task db:status      # goose migration status
task db:seed        # migrate + load demo data (via the web binary)
task db:dsn         # print the DSN the admin targets use
```

The Tailwind entry point and theme tokens live in
`internal/web/tailwind/{input.css,theme.css}`.

Web listen address comes from `--addr`, else config `web.addr` (default `:8080`).

**DB DSN for the web binary:** the Postgres DSN comes from the `--db` flag, else
`BUDGET_DB_DSN`, else `budget.yaml`'s `db.dsn` (not checked-in), else the
built-in local default (same URL). Because `budget.yaml` points at the **real dev
database**, running `go run ./cmd/web` from the repo root migrates and mutates it.
For any throwaway/smoke run, pass `--db postgres://.../<scratch_db>` so you don't
touch real data.

**Tests require Postgres.** The suite uses a shared `budget_test` database, reset
per-test via `TRUNCATE ... RESTART IDENTITY CASCADE`. The DSN comes from
`BUDGET_POSTGRES_URL` (default `postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable`);
Postgres is required — tests do not skip. `go test ./...` runs package binaries in
parallel, so every DB-backed test grabs one global Postgres advisory lock to
serialize access to the shared DB.

## Architecture

```
cmd/web/main.go            Web binary entrypoint (opens db, builds store, serves Gin)

internal/cli/              Shared Cobra/Viper CLI: root flags, config, db/migrate/seed
                           subcommands. Imports only internal/core, never a UI package.

internal/core/store/       Persistence layer — raw SQL with native Postgres `$1,$2`
                           placeholders. One file per aggregate: accounts,
                           budgets, categories, incomes, transactions.
internal/core/db/          Postgres open (pgx) + embedded goose migrations
                           (migrations/postgres/).
internal/core/config/      Viper config (budget.yaml / BUDGET_* env / CLI flags).
internal/core/money/       Integer cents <-> human string parsing/formatting.
internal/core/format/      Presentation helpers shared by TUI + web (goal/date wording).
internal/core/paydown/     Debt amortization projection (pure Go, no DB).

internal/web/              Web app.
  server.go                Gin router + embedded static FS. Serves templUI
                           component JS at /templui/js/*.min.js from the
                           templui module's embedded TemplFiles.
  handlers/                HTTP handlers (budget, accounts, categories, income,
                           transactions, paydown; render/stub helpers).
  views/                   Templ templates (.templ) + generated *_templ.go.
  tailwind/                input.css (Tailwind entry) + theme.css (OKLCH tokens).
  static/                  app.css (Tailwind build), htmx.min.js, favicon.
```

The web UI's shared components come from upstream
`github.com/templui/templui` (button, table, card, badge, input, checkbox,
dialog, label, selectbox, ...), styled via Tailwind v4.

## Key Patterns

- **Amounts are integer cents everywhere** in code; formatting happens at the
  boundary via the `money` / `format` packages.
- **Store**: all SQL uses native Postgres `$1,$2,...` placeholders. Construct
  with `store.New(db)`. Bind Go bools directly (`true`/`false`), never integer
  literals — Postgres rejects int→boolean.
- **Migrations** live in `internal/core/db/migrations/postgres/`.
- **Config precedence**: CLI flag → `BUDGET_*` env → `budget.yaml` → defaults.
- **Web is server-rendered**: Templ generates Go; HTMX drives partial updates.
  After editing a `.templ` file, run `task templ` (or `task test`, which does it)
  before building — the `*_templ.go` files are checked in.

## Key Rules

- NEVER commit API keys or secrets — use environment variables.
- Run `task test` before considering backend/core work done.
- After editing `.templ` or Tailwind sources, regenerate (`task templ` / `task css`).
- Postgres-only: all new store SQL uses `$1,$2,...`; add migrations under
  `migrations/postgres/`.
- Do not suggest or create commits, merges, or PRs — the user does all of this.
