<div align="center">

# budget

**A local-first personal finance app — use it in your terminal or your browser.**

Track accounts, assign money to categories each month, project debt paydown, and see where your money goes — all backed by a local SQLite file (or Postgres) with no accounts, no sync, and no subscription.

![Budget tab](./screenshots/01-budget.png)

</div>

---

## Two interfaces, one database

`budget` ships with two fully-featured interfaces that read and write the same database:

| | TUI | Web |
|---|---|---|
| **Launch** | `./bin/tui/budget` | `./bin/web/budget` |
| **Access** | Terminal | Browser at `http://localhost:8080` |
| **Best for** | Local daily use, keyboard power users | Remote access, Docker/Synology, mouse-friendly use |
| **Navigation** | Keyboard shortcuts | Click tabs, real URLs (browser back button works) |
| **Updates** | Full screen redraws | HTMX partial swaps — only changed rows update |
| **Theme** | Catppuccin Mocha (terminal colors) | Catppuccin Mocha (CSS) |

Both interfaces cover all tabs: Budget · Transactions · Accounts · Categories · Paydown.

---

## Features

- **Envelope budgeting** — assign money to categories each month; available balance carries forward (positive only)
- **Multi-account** — checking, savings, cash, credit cards, and loans in one view
- **Transfers** — move money between accounts; tag the from-leg with a budget category (e.g. paying a credit card)
- **Debt paydown projector** — real APR daily-compound amortization; links to your budget so actual payments replace forecasts automatically
- **Sinking-fund goals** — set a target amount + due date; the app tells you the monthly contribution needed
- **Income tracking** — estimate income for each month; see estimated vs. actual side-by-side on the Budget tab
- **Fully local** — one SQLite file, no network access, no accounts, no telemetry

---

## Screenshots — TUI

**Budget** — assign money to categories each month, track against income, see sinking-fund goals

![Budget](./screenshots/01-budget.png)

**Income panel** — estimate income per source; see estimated vs. actual side-by-side

| Income panel | Edit income | Assign amount |
|---|---|---|
| ![Income panel](./screenshots/02-budget-income-panel.png) | ![Edit income](./screenshots/03-budget-income-edit.png) | ![Assign](./screenshots/04-budget-assign.png) |

---

**Transactions** — every account in one list; filter by account or month

| All accounts | Filtered to Chase Sapphire |
|---|---|
| ![Transactions](./screenshots/05-transactions.png) | ![Filtered](./screenshots/12-transactions-filtered.png) |

Transaction form with calendar date picker and account/category pickers:

| Edit transaction | New transaction | Date picker |
|---|---|---|
| ![Edit](./screenshots/06-transaction-edit.png) | ![New](./screenshots/07-transaction-new.png) | ![Date picker](./screenshots/08-datepicker.png) |

| Account picker | Category picker | Account filter |
|---|---|---|
| ![Account picker](./screenshots/09-account-picker.png) | ![Category picker](./screenshots/10-category-picker.png) | ![Account filter](./screenshots/11-account-filter.png) |

---

**Accounts** — net worth at a glance; credit and loan balances with APR

| Account list | Edit account |
|---|---|
| ![Accounts](./screenshots/13-accounts.png) | ![Edit account](./screenshots/14-account-edit.png) |

---

**Categories** — grouped categories with optional sinking-fund goals

| Category list | Edit category (goal) | Edit category (sinking fund) |
|---|---|---|
| ![Categories](./screenshots/15-categories.png) | ![Goal](./screenshots/17-category-edit-goal.png) | ![Sinking fund](./screenshots/18-category-edit-sinking-fund.png) |

---

**Paydown** — daily-compound debt amortization linked to your budget categories

![Paydown](./screenshots/19-paydown.png)

---

## Screenshots — Web

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

**Requirements:** [Go 1.21+](https://go.dev/dl/)

```bash
git clone https://github.com/sbengtson/budget
cd budget
make setup    # download deps + install goose
make seed     # load 3 months of realistic demo data
make run      # launch the TUI
```

To open the web interface instead:

```bash
make seed     # if you haven't already
./bin/web/budget          # serves http://localhost:8080
```

To start fresh with your own data:

```bash
make db-reset  # wipe the database
make run       # TUI — auto-migrates on first open
# or
./bin/web/budget  # web — auto-migrates on first request
```

---

## Usage

There are two binaries, both named `budget`, built to `./bin/tui/budget` and
`./bin/web/budget`. With no subcommand each launches its own interface; both
share the same Cobra admin subcommands (`config`, `db`, `migrate`), so you can
run schema and data tasks from whichever binary is handy. Examples below use
`./bin/tui/budget`.

```bash
./bin/tui/budget                     # TUI (default for the tui binary)
./bin/web/budget                     # HTTP server on :8080 (default for the web binary)
./bin/web/budget web                 # explicit web launch (same as bare web binary)
./bin/tui/budget migrate --from <a> --to <b>   # copy data SQLite ↔ Postgres
./bin/tui/budget config show         # print resolved config

# Schema + seed (under `db` group; available from either binary):
./bin/tui/budget db up               # apply all pending up migrations
./bin/tui/budget db up-one           # apply just the next pending migration
./bin/tui/budget db down             # roll back the most recent migration
./bin/tui/budget db reset            # roll back to zero + re-apply (DESTRUCTIVE)
./bin/tui/budget db status           # one line per migration (applied / pending)
./bin/tui/budget db version          # current migration version
./bin/tui/budget db seed             # populate demo data
```

> Note the two different "migrate" verbs:
> - `budget db ...` runs **schema** migrations (the goose up/down/status workflow).
> - `budget migrate --from --to` copies **data** between two databases.

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

### Migrating between SQLite and Postgres

```bash
budget migrate \
  --from ./data/budget.db \
  --to   postgres://postgres:postgres@127.0.0.1:5432/budget?sslmode=disable
```

The destination is wiped (TRUNCATE on Postgres / DELETE on SQLite) and primary keys are preserved. Postgres sequences advance past the imported max id so future inserts continue from there.

---

## Development

```bash
make setup      # install Go module deps + goose CLI
make build      # compile both binaries to ./bin/tui/budget and ./bin/web/budget
make test       # run the full test suite
make seed       # load demo data into ./data/budget.db
make clean      # remove the compiled binary
```

Database commands:

```bash
make db-path    # print the configured database path
make db-migrate # run pending migrations (goose up)
make db-reset   # delete the database and re-run migrations
make db-status  # show goose migration status
make db-delete  # delete the database file (and WAL/SHM)
```

---

## Keymap

**Global**

| Key | Action |
|---|---|
| `1`–`5` / click | switch tabs |
| `shift+h` / `shift+l` | prev / next tab |
| `q` / `ctrl+c` | quit |
| `?` | show / hide help |
| `esc` | cancel form or modal |

**List views** (Accounts, Categories, Transactions)

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` | move cursor |
| `n` | new |
| `enter` | edit selected |
| `d` | archive / delete (with confirm) |

**Transactions**

| Key | Action |
|---|---|
| `c` | toggle cleared on selected |
| `f` / `F` | filter by account / clear filter |
| `<` / `>` | prev / next month filter |
| `t` / `M` | jump to current month / clear month filter |
| `pgup` / `pgdn` | page through long lists |

**Budget tab**

| Key | Action |
|---|---|
| `↑` `↓` | move cursor |
| `enter` | edit assigned amount for selected category |
| `g` | set goal + due date |
| `i` | open income panel for the month |
| `<` / `>` | prev / next month |
| `t` | jump to current month |

Income panel: `n` new · `enter` edit · `d` delete · `esc` back.
Multiple income lines per month (e.g. Salary, Freelance). The Budget banner shows `Estimated · Actual · Budgeted · Remain · Est−Act`.

**Paydown tab**

| Key | Action |
|---|---|
| `↑` `↓` | select account |
| `a` | add account to plan (must have APR set) |
| `e` | edit monthly payment for selected account |
| `c` | link a budget category to selected account |
| `r` / `d` | remove selected account from plan |
| `+` / `-` | extend / shrink projection by 12 months |
| `,` / `.` | page through projection rows |

Each included account projects monthly amortization at `APR / 365` daily compounding. If a payment is below the first month's interest the row flags `payment ≤ interest, debt grows`.

**Variable payments:** for every projected month the engine picks the payment in this order:
1. **spent** — actual outflow against the linked category that month
2. **assigned** — the budgeted amount for that month
3. **default** — the account's fixed monthly payment fallback

The `Source` column labels each row (`✓ spent`, `→ assigned`, `· default`).

**Forms**

`tab` / `↑↓` moves between fields; `enter` advances or saves on the last field; `space` opens a picker on Type / Account / Category fields. On the **Date** field in the transaction form, `space` opens a calendar picker — `hjkl` / arrows navigate days, `tab` cycles month → year → day focus, `enter` commits, `esc` cancels.

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
cmd/tui/main.go             tui binary entrypoint (builds to ./bin/tui/budget)
cmd/web/main.go             web binary entrypoint (builds to ./bin/web/budget)
internal/cli/               shared Cobra commands (root flags, config/db/migrate/seed)
internal/core/config/       runtime configuration loading
internal/core/db/           SQLite/Postgres open + embedded goose migrations
internal/core/money/        cents ↔ human string parsing and formatting
internal/core/store/        persistence layer (one file per aggregate)
internal/core/paydown/      debt amortization projection (pure Go, no DB)
internal/core/format/       presentation helpers shared by both UIs (goal summaries)
internal/tui/               Bubble Tea screens and components
internal/web/               Gin + Templ + HTMX web server and handlers
```

Each binary links only its own UI: building the tui pulls in no web
dependencies (Gin/Templ) and the web binary pulls in no terminal-UI
dependencies (Bubble Tea). Both share everything under `internal/core` and the
admin commands in `internal/cli`.
