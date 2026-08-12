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

**The tailwind CLI is invoked through `scripts/tailwind-runnable.sh`, never
directly.** It is a Bun-compiled single-file executable whose Mach-O signature
Bun writes truncated (oven-sh/bun#29120); macOS 27 enforces signatures at exec
and SIGKILLs it, so every invocation exits 137 — including `--help`. The same
broken artifact ships to both the GitHub release and nixpkgs, so devbox is not
an escape. The script hands back an ad-hoc re-signed copy at `bin/tailwindcss`
(the nix store is read-only, hence a copy). Symptom if this ever regresses:
`task css` fails with `exit status 137` and air silently keeps serving the last
binary it built — `tmp/build-errors.log` records only the exit status.

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
task db:reset       # migrate + TRUNCATE every table (DESTRUCTIVE — wipes data, keeps schema)
task db:status      # goose migration status
task db:seed        # migrate + seed the two accounts + demo data (via the web binary)
task db:dsn         # print the DSN the admin targets use
```

`db:reset` empties the data without touching the schema: it truncates every
table in one statement (`RESTART IDENTITY CASCADE`, so the foreign keys between
them don't need a delete order) and leaves `goose_db_version` alone. It then
restores the global Income category that migration 00005 inserts — truncating
removes it and no migration will run again to put it back. It deliberately does
*not* roll migrations back; `budget db reset` still does that if you need it.

`db:seed` creates two verified accounts, both with password `password1`
(`internal/cli/seed.go`). It refuses to run against a database that already has
data, so the flow is `task db:reset` then `task db:seed`.

| Account | Purpose |
| --- | --- |
| `admin@example.com` | Owns the demo data (3 months of transactions). Has `is_admin` and a lifetime complimentary subscription, so `/admin` and the whole app are reachable with no Stripe setup. |
| `test@example.com` | A bare freshly-registered account: starter budget only, no subscription and no comp, so logging in lands on the free-trial flow. |

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
internal/core/money/       Integer cents <-> human string parsing/formatting (locale-aware).
internal/core/format/      Presentation helpers shared by TUI + web (goal/date wording).
internal/core/i18n/        Locale type, go-i18n bundle + embedded TOML catalogs
                           (locales/en-CA.toml, fr-CA.toml), number/date tables.
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
- **All user-facing text comes from the catalog.** Never write an English string
  into a view or handler — add a key to `internal/core/i18n/locales/en-CA.toml`
  and its `fr-CA.toml` counterpart, then call `i18n.T(ctx, "key")` (`Tf` to
  interpolate, `Tn`/`Tnf` to pluralize). A test fails if the two catalogs
  disagree on which keys exist. Put whole sentences in the catalog, never
  fragments concatenated in Go — clause order differs between languages.
- **Public marketing + legal pages are URL-per-language**, not cookie-driven:
  English at `/`, `/privacy`, …; French at `/fr`, `/fr/privacy`, …. `pinLocale`
  fixes the locale from the URL so a visitor's cookie can never change what a
  given address returns — these are the pages that must be indexable and
  shareable per language. Links between them go through `publicHref(ctx, path)`
  to stay in-language; each page emits `hreflang` alternates plus a self
  `canonical`. Adding a public page means adding it to `views.PublicPages` and
  to the `publicHandlers` map in `server.go` — that one list drives route
  registration for every language AND `/sitemap.xml`, so the two cannot drift
  (a listed page with no handler panics at startup). `/robots.txt` is generated
  from the same handler package (it needs the configured origin for its
  `Sitemap:` line) and is allow-by-default — a new `Disallow:` that prefix-
  matches a public page fails `robots_test.go`.
- **Signed-out auth pages are bilingual**, not single-language: a visitor there
  has no stored preference, only a guess from `Accept-Language`. Sign in, sign
  up, forgot and reset render every supported language via `i18n.EveryLocale` —
  short things slash-joined by `bi()`, sentences stacked by `biLines` — viewer's
  locale first. Those views therefore take message **keys**, not rendered
  strings. A stopgap sized for two languages; a third needs a switcher instead.
- **Locale rides the context**, which Templ hands components as `ctx`. Resolved
  for every request by `resolveLocale` (cookie → `Accept-Language`), then
  overwritten by `requireAuth` with the signed-in user's `users.locale`.
- **The first-run wizard owns starter-budget seeding.** `Register` creates the
  user and nothing else; `/welcome` asks for the language first, then seeds the
  categories in it (`store.SeedNewUser(ctx, uid, locale, groupKeys)`). Those
  names become the user's own editable rows, so they are translated once at
  seeding and never re-translated on a later language switch.
  `users.onboarded_at` gates the app via `requireOnboarded`; the wizard's own
  routes sit outside that gate.
- **`money.Format`/`Parse` and the `format` date helpers take `ctx`** — en-CA is
  `$1,234.56` and fr-CA is `1 234,56 $` with U+00A0 separators, and Go's
  `time.Format` cannot localize month names at all, so never reach for a layout
  string for anything a user reads.

## Key Rules

- NEVER commit API keys or secrets — use environment variables.
- Run `task test` before considering backend/core work done.
- After editing `.templ` or Tailwind sources, regenerate (`task templ` / `task css`).
- Postgres-only: all new store SQL uses `$1,$2,...`; add migrations under
  `migrations/postgres/`.
- Do not suggest or create commits, merges, or PRs — the user does all of this.
