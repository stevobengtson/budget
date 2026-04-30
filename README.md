# Budget

A small local terminal budget app: accounts, transactions, monthly envelope budgets, credit-card debt tracking, and sinking-fund savings. SQLite-backed, single user, no auth.

## Run

```bash
go run ./cmd/budget               # opens ~/.config/budget/budget.db
go run ./cmd/budget --db ./b.db   # custom DB path
```

Build a binary:

```bash
go build -o budget ./cmd/budget
./budget
```

## Tests

```bash
go test ./...
```

## Keymap

Global

| key            | action            |
|----------------|-------------------|
| `1`–`7` / click   | switch tabs (mouse on the tab bar works too) |
| `shift+h` / `shift+l` | prev / next tab                          |
| `q` / `ctrl+c` | quit              |
| `esc`          | cancel form/modal |

List views (Accounts, Categories, Transactions)

| key   | action                          |
|-------|---------------------------------|
| `↑↓`  | move cursor                     |
| `n`   | new                             |
| enter | edit selected                   |
| `d`   | archive / delete (with confirm) |
| `c`   | (Transactions) toggle cleared   |
| `f` / `F` | (Transactions) filter by account / clear filter |
| `<` / `>` | (Transactions) prev / next month filter |
| `t` / `M` | (Transactions) jump to current month / clear month filter |
| `pgup` / `pgdn` (or `home` / `end`) | (Transactions) page through long lists |

Budget tab

| key   | action                                  |
|-------|-----------------------------------------|
| `↑↓`  | move cursor                             |
| `</>` | prev / next month                       |
| `t`   | jump to current month                   |
| `e`   | edit assigned amount                    |
| `g`   | set goal + due date                     |
| `i`   | manage income for the month (sub-panel) |

Income panel: `n` new · enter edit · `d` delete · esc back. Multiple income items per month (e.g. Work, Government, Contract). Banner on the budget view always shows `Income · Budgeted · Remain`.

Paydown tab

| key   | action                                       |
|-------|----------------------------------------------|
| `↑↓`  | select account section (also click)          |
| `a`   | add an account (must have APR set)           |
| `e`   | edit monthly payment for selected account    |
| `r` / `d` | remove selected account from paydown     |
| `+` / `-` | extend / shrink projection by 12 months  |
| `,` / `.` (or `pgup` / `pgdn`) | page through projection rows (12 / page) |
| `home` / `end` | jump to first / last page           |

Each included account projects monthly amortization at `APR / 365` daily compounding for the days in each calendar month. Starting balance comes from the account's current owed amount (negative running balance for `credit`/`loan` accounts). Banner shows total monthly outflow, total interest, and the longest payoff horizon. If a payment is below the first month's interest the section flags `payment ≤ interest, debt grows`.

**Variable monthly payments.** Each paydown account can be linked to a budget category (e.g. "Visa Payment"). For every projected month the engine picks the payment in this order:

1. **spent** — actual outflow recorded against that category in that month, if > 0
2. **assigned** — the assigned budget amount for that month, if > 0
3. **default** — the account's `monthly_payment_cents` fallback

The `Source` column on each row labels which datum was used (`✓ spent`, `→ assigned`, `· default`). To raise May's Visa payment to $1,000, just assign $1,000 to `Visa Payment` for May on the Budget tab — paydown picks it up immediately. Once the payment lands as a real transaction it switches from `assigned` to `spent`.

Reports tab

| key       | action                                          |
|-----------|-------------------------------------------------|
| `s`       | spending by category (horizontal bar chart)     |
| `c`       | monthly cashflow (12 months, income vs expense) |
| `[` / `]` | prev / next period for spending (month / 30d / 90d / YTD) |
| `r`       | refresh                                         |

Charts render via [ntcharts](https://github.com/NimbleMarkets/ntcharts) `barchart`. Numbers shown beside the chart for precision. Cashflow uses actual transaction inflow when present; falls back to configured incomes for the month otherwise.

In forms, `tab`/`↑↓` moves between fields, `enter` advances or saves on the last field, and `space` opens a picker on Type/Account/Category fields. In the transaction form, `space` on the **Date** field opens the [bubble-datepicker](https://github.com/EthanEFung/bubble-datepicker) calendar — `hjkl`/arrows nav days, `tab` cycles month/year/calendar focus, `enter` commits, `esc` cancels.

## Concepts

- **Accounts**: `checking`, `savings`, `cash`, `credit`, `loan`. Credit cards have negative balance when you owe (purchases = outflow). Pay a card with a transfer from checking → CC.
- **Categories** are grouped (`Monthly`, `Annual`, etc.) and can carry a sinking-fund goal (`goal_cents` + `goal_due_date`).
- **Budget** is per-month: assign a dollar amount per category. `Available = carryover (≥ 0) + assigned − spent`. Sinking-fund categories show the monthly contribution required to hit the goal.
- **Amounts** are stored as integer cents. The TUI accepts `1234.56`, `$1,234.56`, `-50`, etc.
- **Liability payments**: a transfer can carry a category. The category attaches to the **from-leg** (the side that represents the spending event) so the budget envelope reflects the payment. Standard pattern for paying down a credit card or line of credit: in the transaction form set both **Category** = `CC Payment` and **Transfer to** = `Visa`. The to-leg stays uncategorized so inflow doesn't double-count. Interest charges that the bank books directly against the card stay as a plain non-transfer outflow on the card with no category.
- **Liability starting balance**: enter the amount **owed** as a positive number (e.g. `5000` for a $5,000 credit card balance). The form auto-negates it for `credit` and `loan` types so ledger math stays consistent (paying down a card increases the stored balance toward zero).
- **Income category**: a system-managed `Income` category is seeded automatically. Categorize paycheck inflows here. Cannot be edited or deleted. The Budget tab shows two values — `Estimated` (manual forecasts via `i`) and `Actual` (sum of real inflows categorized as Income for the month). `Remain` = Actual − Budgeted.

## UI bits

- **Tabs** are real Lipgloss tabs (active tab merges into the body below).
- **Mouse** is wired via [bubblezone](https://github.com/lrstanley/bubblezone). Click a tab to switch; click any row in Accounts / Transactions / Categories / Budget / Income panel to select it (then `enter` to edit, `d` to delete, etc.). Mouse cell motion is enabled (`tea.WithMouseCellMotion`).
- **Status bar** at the bottom shows the active mode, a context-specific keymap, and the latest flash on the right. A `bubbles/spinner` ticks for ~700 ms after each save / delete to confirm the action.
- Forms / pickers / confirms all share styled rounded panels.

## Layout

```
cmd/budget/main.go         entrypoint
internal/db                SQLite open + goose migrations (embedded)
internal/money             cents <-> human string
internal/store             persistence layer (one file per aggregate)
internal/paydown           debt amortization projection (pure Go, no DB)
internal/tui               Bubble Tea screens
```
