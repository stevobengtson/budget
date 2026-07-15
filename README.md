# budget

**A local-first personal finance app — use it in your terminal or your browser.**

Track accounts, assign money to categories each month, project debt paydown, and see where your money goes — all backed by a local SQLite file (or Postgres) with no accounts, no sync, and no subscription.

---

## Features

- **Envelope budgeting** — assign money to categories each month; available balance carries forward (positive only)
- **Multi-account** — checking, savings, cash, credit cards, and loans in one view
- **Transfers** — move money between accounts; tag the from-leg with a budget category (e.g. paying a credit card)
- **Debt paydown projector** — real APR daily-compound amortization; links to your budget so actual payments replace forecasts automatically
- **Sinking-fund goals** — set a target amount + due date; the app tells you the monthly contribution needed
- **Income tracking** — estimate income for each month; see estimated vs. actual side-by-side on the Budget tab

---

## Screenshots

The web interface mirrors every TUI tab with the same Catppuccin Mocha theme. Each page has a real URL so the browser back button and bookmarks work.

**Budget** — same envelope view; click a category row to assign or set a goal

![Web Budget](./screenshots/web-01-budget.png)

---

**Transactions** — sortable list with account and month filters; inline cleared toggle

![Web Transactions](./screenshots/web-02-transactions.png)

---

**Accounts** — net worth summary; edit or archive accounts inline

![Web Accounts](./screenshots/web-03-accounts.png)

---

**Categories** — grouped list; edit names, goals, and sinking-fund targets

![Web Categories](./screenshots/web-04-categories.png)

---

**Paydown** — include/exclude accounts, set payments, link budget categories

![Web Paydown](./screenshots/web-05-paydown.png)

---

## Quick start

**Requirements:** [Task](https://taskfile.dev) + [Go 1.26+](https://go.dev/dl/) — or nothing but [Devbox](https://www.jetify.com/devbox) (see below), which brings both.

```bash
git clone https://github.com/sbengtson/budget
cd budget
task setup    # download deps + install goose
task db:seed  # load 3 months of realistic demo data
task run      # launch the application
```

Run `task` with no args for the full task list.

### With Devbox (no host toolchain)

[Devbox](https://www.jetify.com/devbox) provisions an isolated shell with the
exact pinned tool versions (Go, Task, templ, tailwind, goose, air, sqlite) — no
Docker, no global installs. `devbox.json` is the single source of truth for those
versions.

```bash
devbox shell   # enter the shell; tools land on PATH, versions print on entry
task db:seed   # same tasks work inside
task run       # launch the application
```

Or run a target without entering the shell:

```bash
devbox run web     # scripts: web · dev · test · migrate · seed
```

Inside the shell, Task uses the Devbox-provided binaries (via `TAILWIND`/`TEMPL`
env), so the tailwind download and `go install` steps are skipped.

To open the web interface instead:

```bash
task db:seed              # if you haven't already
./bin/web/budget          # serves http://localhost:8080
```

To start fresh with your own data:

```bash
task db:reset  # wipe the database
task run       # web — auto-migrates on first open
# or
./bin/web/budget  # web — auto-migrates on first request
```

---

## Usage

With no subcommand each launches its own interface; both
share the same Cobra admin subcommands (`config`, `db`, `migrate`), so you can
run schema and data tasks from whichever binary is handy.

```bash
./bin/web/budget                     # HTTP server on :8080 (default for the web binary)
./bin/web/budget web                 # explicit web launch (same as bare web binary)
./bin/web/budget config show         # print resolved config

# Schema + seed (under `db` group; available from either binary):
./bin/web/budget db up               # apply all pending up migrations
./bin/web/budget db up-one           # apply just the next pending migration
./bin/web/budget db down             # roll back the most recent migration
./bin/web/budget db reset            # roll back to zero + re-apply (DESTRUCTIVE)
./bin/web/budget db status           # one line per migration (applied / pending)
./bin/web/budget db version          # current migration version
./bin/web/budget db seed             # populate demo data
```

Persistent flags on the root command:

| flag           | meaning                                               |
|----------------|-------------------------------------------------------|
| `--db <dsn>`   | SQLite path or `postgres://...` URL                   |
| `--config <f>` | explicit config file (overrides search path)          |
| `--log-level`  | `debug` / `info` / `warn` / `error`                   |

### Configuration

Settings are resolved (highest precedence first) from CLI flag → `BUDGET_*` env var → config file → defaults. The config file is YAML and looked up in:

1. `./budget.yaml`
2. `$XDG_CONFIG_HOME/budget/config.yaml`
3. `~/.config/budget/config.yaml`

Sample (`budget.example.yaml`):

```yaml
db:
  dsn: "./data/budget.db"     # or postgres://user:pw@host:5432/db
web:
  addr: ":8080"
log:
  level: "info"
```

### Web interface

`budget web` serves an HTMX + Templ + Gin frontend. It mirrors every TUI tab and uses real URLs (e.g. `/budget?month=2026-05`) so the browser back button works. Forms swap individual rows and sections via HTMX — the page never fully reloads. The Catppuccin Mocha theme matches the TUI.

```bash
./bin/web/budget --addr :8080
open http://localhost:8080
```

The web app reads its database from the same config the TUI uses. You can run both simultaneously against the same SQLite file; each request reads fresh data.

### Docker / Apple Containers

A multi-stage `Dockerfile` is included. The image runs `budget web` by default and listens on `:8080`. Both database drivers are pure Go so the binary is fully static — no extra packages required at runtime.

```bash
# Build (Apple `container` CLI shown; works the same with docker / podman)
container build -t budget:latest .

# Postgres connection (Synology layout, where postgres lives in another container)
container run --rm -p 8080:8080 \
  -e BUDGET_DB_DSN='postgres://postgres:postgres@postgres:5432/budget?sslmode=disable' \
  -e BUDGET_WEB_ADDR=':8080' \
  budget:latest

# SQLite — bind a local directory at /data and point the DSN at it
container run --rm -p 8080:8080 \
  -v /volume1/budget:/data \
  -e BUDGET_DB_DSN=/data/budget.db \
  budget:latest
```

A starter `docker-compose.example.yml` is also included with both the `budget` service and an optional Postgres sidecar — drop it next to your existing Synology compose stack and adjust the DSN to match your Postgres container's hostname.

Migrations apply automatically on first connect, so a fresh Postgres database becomes a fully-set-up budget DB on first request. Use `container run ... budget db status` (or any other `db ...` subcommand) to inspect or roll back schema changes.

---

## Development

```bash
task setup      # install Go module deps + goose CLI
task build      # compile both binaries to ./bin/tui/budget and ./bin/web/budget
task test       # run the full test suite
task db:seed    # load demo data into ./data/budget.db
task clean      # remove the compiled binary
```

Database commands:

```bash
task db:path    # print the configured database path
task db:migrate # run pending migrations (goose up)
task db:reset   # delete the database and re-run migrations
task db:status  # show goose migration status
task db:delete  # delete the database file (and WAL/SHM)
```

---

## Concepts

**Accounts** — `checking`, `savings`, `cash`, `credit`, `loan`. Credit and loan accounts carry a negative running balance when in debt; purchases are outflows and payments are inflows (via transfer). Net worth = assets + liabilities.

**Categories** are grouped and can carry a sinking-fund goal (`goal amount` + `due date`). The Budget tab shows how much to contribute each month to reach the goal on time.

**Envelope budgeting** — each month you assign money to categories. Available = `carryover (≥ 0 from prior month) + assigned − spent`. Unspent money rolls forward; overspending does not.

**Transfers** — moving money between accounts records two linked transactions. A category can be attached to the from-leg (e.g. "CC Payment") so the spending shows in your budget without double-counting the inflow.

**Liability starting balance** — enter the amount owed as a positive number (e.g. `2500` for a $2,500 credit card balance). The form automatically stores it as negative so ledger math stays consistent.

**Income category** — a system-managed `Income` category is seeded automatically on first run. Categorize paycheck inflows here. The Budget tab shows `Estimated` (manual forecasts entered via `i`) vs. `Actual` (real inflows categorised as Income).

**Amounts** — stored as integer cents. The input parser accepts `1234.56`, `$1,234.56`, `1234`, `-50`, `.5`, etc.

---

## Layout

```
cmd/web/main.go             web binary entrypoint (builds to ./bin/web/budget)
internal/cli/               shared Cobra commands (root flags, config/db/migrate/seed)
internal/core/config/       runtime configuration loading
internal/core/db/           SQLite/Postgres open + embedded goose migrations
internal/core/money/        cents ↔ human string parsing and formatting
internal/core/store/        persistence layer (one file per aggregate)
internal/core/paydown/      debt amortization projection (pure Go, no DB)
internal/core/format/       presentation helpers shared by both UIs (goal summaries)
internal/web/               Gin + Templ + HTMX web server, handlers, and views
                            (UI components from github.com/templui/templui)
```

Each binary links only its own UI: building the tui pulls in no web
dependencies (Gin/Templ) and the web binary pulls in no terminal-UI
dependencies (Bubble Tea). Both share everything under `internal/core` and the
admin commands in `internal/cli`.
