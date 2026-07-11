# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## Project Overview

Local-first personal finance app with envelope budgeting. **Single Go module**
(`github.com/sbengtson/budget`) that builds **two binaries sharing one core**:

- **TUI** (`cmd/tui`) — keyboard-driven terminal UI (Bubble Tea + Lipgloss).
- **Web** (`cmd/web`) — server-rendered web UI (Gin + HTMX + Templ).

Both link the same `internal/core/*` packages and the same database. SQLite is
the primary/local store; the store layer also supports Postgres via a dialect
abstraction. No separate JS frontend — the web UI is Go templates + HTMX.

## Build & Run

All commands run from the repo root via the `Makefile`.

```bash
make setup          # go mod download + install goose; installs templ + tailwind CLIs
make build          # build both binaries -> bin/tui/budget, bin/web/budget
make run            # build + launch TUI (alias: make tui)
make web            # build + launch web server (regenerates css + templ first)
make dev            # hot-reload via air
make test           # templ generate + go test ./...

# Single test:
go test ./internal/core/store/ -run TestAccountCRUD -v

# Codegen (needed after editing .templ files or Tailwind sources):
make templ          # templ generate for internal/web + pkg/shadcntempl
make css            # compile Tailwind -> internal/web/static/app.css
make tailwind-watch # watch mode

# Database (SQLite, default DB_PATH=./budget.db):
make db-migrate     # goose up
make db-reset       # delete DB + re-migrate
make db-status      # goose migration status
make seed           # migrate + load demo data (via TUI binary)

# shadcn theme preset -> pkg/shadcntempl/theme/theme.css:
make theme PRESET=<id>      # or: make theme URL=<themes json url>
```

Web listen address comes from `--addr`, else config `web.addr` (default `:8080`).

## Architecture

```
cmd/tui/main.go            TUI binary entrypoint (Bubble Tea)
cmd/web/main.go            Web binary entrypoint (opens db, builds store, serves Gin)
cmd/shadcntempl-theme/     Tool: fetch a shadcn theme preset -> theme.css

internal/cli/              Shared Cobra/Viper CLI: root flags, config, db/migrate/seed
                           subcommands. Imports only internal/core, never a UI package.

internal/core/store/       Persistence layer — raw SQL with `?` placeholders + dialect
                           rebind (SQLite/Postgres). One file per aggregate: accounts,
                           budgets, categories, incomes, transactions.
internal/core/db/          DB open, dialect detect, embedded goose migrations
                           (migrations/{sqlite,postgres}/ — keep both in sync).
internal/core/config/      Viper config (budget.yaml / BUDGET_* env / CLI flags).
internal/core/money/       Integer cents <-> human string parsing/formatting.
internal/core/format/      Presentation helpers shared by TUI + web (goal/date wording).
internal/core/paydown/     Debt amortization projection (pure Go, no DB).

internal/tui/              Bubble Tea screens/components (accounts, budget, categories,
                           transactions, paydown, forms, styles, bootstrap).

internal/web/              Web app.
  server.go                Gin router + embedded static FS.
  handlers/                HTTP handlers (budget, accounts, categories, income,
                           transactions, paydown; render/stub helpers).
  views/                   Templ templates (.templ) + generated *_templ.go.
  static/                  app.css (Tailwind build), htmx.min.js, favicon.

pkg/shadcntempl/           Reusable shadcn/ui-style Templ + Tailwind v4 component library
                           (button, table, card, badge, input, checkbox, dialog, label,
                           selectbox). One subpackage per component; theme/ holds OKLCH
                           token blocks. Vendored, intended to be extractable as its own module.
```

## Key Patterns

- **Amounts are integer cents everywhere** in code; formatting happens at the
  boundary via the `money` / `format` packages.
- **Store**: all SQL uses `?` placeholders; the dialect helper rebinds for
  Postgres. Construct with `store.New(db)` (SQLite) or `store.NewWithDialect`.
- **Migrations** live in `internal/core/db/migrations/{sqlite,postgres}/` —
  add schema changes to **both** dirs.
- **Config precedence**: CLI flag → `BUDGET_*` env → `budget.yaml` → defaults.
- **Web is server-rendered**: Templ generates Go; HTMX drives partial updates.
  After editing a `.templ` file, run `make templ` (or `make test`, which does it)
  before building — the `*_templ.go` files are checked in.
- **Shared core, thin UIs**: `internal/cli` and `internal/core` carry no UI deps;
  TUI and web are the only UI packages.

## Key Rules

- NEVER commit API keys or secrets — use environment variables.
- Run `make test` before considering backend/core work done.
- After editing `.templ` or Tailwind sources, regenerate (`make templ` / `make css`).
- Keep SQLite and Postgres migration directories in sync.
- Do not suggest or create commits, merges, or PRs — the user does all of this.
