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

All commands run from the repo root via [Task](https://taskfile.dev)
(`Taskfile.yml`). Run `task` with no args to list targets. (The old `Makefile`
was removed in favor of Task on 2026-07-12.)

**Devbox (preferred toolchain).** `devbox.json` pins the exact tool versions
(go, go-task, templ, goose, air, tailwindcss_4, sqlite) — the single source of
truth, matched to `go.mod` + the Taskfile. Work inside `devbox shell` (or `devbox
run <script>`; scripts: `web`, `tui`, `dev`, `test`, `migrate`, `seed`). The
shell's `init_hook` exports `TAILWIND`/`TEMPL` to the nix binaries, so Task uses
them and **skips the tailwind download + `go install` steps**. `.air.toml` uses
`${TAILWIND}`/`${TEMPL}` with fallbacks so `task dev` works in and out of Devbox.
No CGO — `modernc.org/sqlite` is pure Go, so no compiler in the shell.
When adding a tool (later: postgres, mailpit), `devbox add <pkg>` and pin it here.

```bash
task setup          # go mod download + install goose; installs templ + tailwind CLIs
task build          # build both binaries -> bin/tui/budget, bin/web/budget
task run            # build + launch TUI (alias: task tui)
task web            # build + launch web server (regenerates css + templ first)
task dev            # hot-reload via air
task test           # templ generate + go test ./...

# Single test:
go test ./internal/core/store/ -run TestAccountCRUD -v

# Codegen (needed after editing .templ files or Tailwind sources):
task templ          # templ generate for internal/web
task css            # compile Tailwind -> internal/web/static/app.css
task tailwind-watch # watch mode

# Database (SQLite, default DB_PATH=./data/budget.db):
task db:migrate     # goose up
task db:reset       # delete DB + re-migrate
task db:status      # goose migration status
task db:seed        # migrate + load demo data (via TUI binary)
```

The Tailwind entry point and theme tokens live in
`internal/web/tailwind/{input.css,theme.css}`.

Web listen address comes from `--addr`, else config `web.addr` (default `:8080`).

## Architecture

```
cmd/tui/main.go            TUI binary entrypoint (Bubble Tea)
cmd/web/main.go            Web binary entrypoint (opens db, builds store, serves Gin)

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
- **Store**: all SQL uses `?` placeholders; the dialect helper rebinds for
  Postgres. Construct with `store.New(db)` (SQLite) or `store.NewWithDialect`.
- **Migrations** live in `internal/core/db/migrations/{sqlite,postgres}/` —
  add schema changes to **both** dirs.
- **Config precedence**: CLI flag → `BUDGET_*` env → `budget.yaml` → defaults.
- **Web is server-rendered**: Templ generates Go; HTMX drives partial updates.
  After editing a `.templ` file, run `task templ` (or `task test`, which does it)
  before building — the `*_templ.go` files are checked in.
- **Shared core, thin UIs**: `internal/cli` and `internal/core` carry no UI deps;
  TUI and web are the only UI packages.

## Key Rules

- NEVER commit API keys or secrets — use environment variables.
- Run `task test` before considering backend/core work done.
- After editing `.templ` or Tailwind sources, regenerate (`task templ` / `task css`).
- Keep SQLite and Postgres migration directories in sync.
- Do not suggest or create commits, merges, or PRs — the user does all of this.
