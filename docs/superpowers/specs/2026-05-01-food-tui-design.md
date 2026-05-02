# food-tui Design Spec

**Date:** 2026-05-01  
**Status:** Approved

## Context

Foodl is a full-stack calorie tracking web app (React + Go/Gin + PostgreSQL) with login/subscriptions/OAuth. The goal is to extract the core food tracking functionality into a local TUI application — no auth, no subscriptions, single-user, SQLite-backed — following the same architecture and patterns as the budget TUI app.

## What We're Building

A terminal UI food diary app at `/Users/steven/Projects/personal/food-tui`. Full feature parity with foodl (minus auth/subscriptions/admin): meal logging, quick meals, calorie goals, weight tracking, and reports with line charts.

## Architecture

New Go module mirroring the budget app structure:

```
food-tui/
  cmd/food-tui/main.go      # entrypoint: flags, DB open, bubblezone init, program launch
  internal/
    db/                      # SQLite connection + goose embedded migrations
    store/                   # persistence layer, one file per aggregate
      store.go               # base Store struct, null helpers
      meals.go               # Create, Update, Delete, ListByDate, GetDailySummary
      quickmeals.go          # Create, Update, Delete, List
      goals.go               # Get, Upsert (single-row)
      weights.go             # Create, Update, Delete, List, ListByRange
    tui/
      app.go                 # root model: date nav + tab routing
      log.go                 # Log tab (meals for selected date)
      quickmeals.go          # Quick Meals tab
      goals.go               # Goals tab
      weight.go              # Weight tab
      reports.go             # Reports tab (ntcharts line chart + table)
      forms.go               # shared form, picker, confirm components
      styles.go              # Lipgloss Catppuccin Mocha theme
  go.mod
```

**DB location:** `~/.config/food-tui/food.db` (respects `XDG_CONFIG_HOME`)

## Dependencies

Same as budget app plus one addition:

| Dep | Purpose |
|-----|---------|
| `charmbracelet/bubbletea` | TUI framework (MVU loop) |
| `charmbracelet/bubbles` | Reusable components (textinput, list, etc.) |
| `charmbracelet/lipgloss` | Terminal styling, Catppuccin Mocha theme |
| `lrstanley/bubblezone` | Mouse click zone handling |
| `modernc.org/sqlite` | Pure Go SQLite driver |
| `pressly/goose/v3` | Schema migrations (embedded SQL) |
| `NimbleMarkets/ntcharts` | Line charts for Reports tab |

## Navigation

Two axes in the root model:

1. **Date bar** (top): left/right arrows change selected date; `t` jumps to today
2. **Tab bar**: `1–5` or Tab key to switch tabs; mouse click via bubblezone

Tabs: `Log | Quick Meals | Goals | Weight | Reports`

Global keys: `q` / `ctrl+c` to quit. Child modal keys block global keys (same pattern as budget).

## Data Schema

```sql
-- 00001_init.sql
CREATE TABLE calorie_goals (
  id         INTEGER PRIMARY KEY,
  daily_goal INTEGER NOT NULL
);

CREATE TABLE meals (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  type       TEXT NOT NULL,  -- breakfast|lunch|dinner|snack
  calories   INTEGER NOT NULL,
  quantity   REAL NOT NULL DEFAULT 1.0,
  unit       TEXT NOT NULL DEFAULT 'serving',
  date       TEXT NOT NULL,  -- YYYY-MM-DD
  notes      TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_meals_date ON meals(date);

CREATE TABLE quick_meals (
  id            INTEGER PRIMARY KEY,
  name          TEXT NOT NULL,
  type          TEXT NOT NULL,
  base_calories INTEGER NOT NULL,
  unit          TEXT NOT NULL DEFAULT 'serving',
  notes         TEXT
);

CREATE TABLE weights (
  id          INTEGER PRIMARY KEY,
  weight_lbs  REAL NOT NULL,
  entry_date  TEXT NOT NULL UNIQUE,  -- one entry per day, YYYY-MM-DD
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Screens & Interactions

### Log Tab (date-scoped, default)
- Calorie progress bar: `consumed / goal` with color coding (green → yellow → red)
- Meal list grouped by type: Breakfast / Lunch / Dinner / Snack
- `n` = new meal, `e` = edit selected, `d` = delete (confirm modal), `f` = log from quick meal (avoids conflict with global `q` quit)
- Quick meal picker (`f`): select template → pre-fills form (calories editable before save)
- Form fields: name, type (picker), calories, quantity, unit, notes (date pre-filled to selected date)

### Quick Meals Tab
- List of saved meal templates
- `n` = new, `e` = edit, `d` = delete (confirm modal)
- Form fields: name, type, base_calories, unit, notes

### Goals Tab
- Displays current daily calorie goal
- `e` = edit goal (single integer field)
- No list — single active goal row (upserted)

### Weight Tab (date-scoped)
- Shows weight entry for selected date if it exists
- `n`/`e` = log/edit weight for that date, `d` = delete
- Mini-table below: last 7 entries (date + lbs)

### Reports Tab (range-based, ignores selected date)
- Toggle `c` = Calorie view / `w` = Weight view
- Top: ntcharts line chart — 30-day calorie trend (vs goal line) or weight trend
- Below: table of daily values
- `[` / `]` shift the 30-day window back/forward

## Store Patterns

Following budget app conventions:
- `Create*` returns inserted ID
- `Update*` takes full object, `WHERE id=?`
- `Delete*` handles cascades where needed
- `List*` returns slices with optional filtering
- Error wrapping: `fmt.Errorf("operation: %w", err)`
- Single DB writer (`MaxOpenConns=1`), WAL mode, foreign keys enabled

## TUI Patterns

Following budget app conventions:
- Per-tab mode machines (enum + switch): list → form → picker → confirm
- `modal() bool` on each model to block global keys
- `routeMouse()` in root dispatches click events to active tab
- Flash messages in status bar for success/error feedback
- Shared `forms.go`: `form` struct (textinput stack), `picker` struct (searchable list), `confirmModel`

## Verification

1. `go build ./cmd/food-tui` — compiles clean
2. Run app: date nav works, all 5 tabs accessible via keyboard and mouse
3. Log tab: create/edit/delete meal, quick meal picker flow works
4. Daily summary calorie bar updates correctly after meal CRUD
5. Quick Meals tab: full CRUD
6. Goals tab: set goal, visible in Log tab progress bar
7. Weight tab: log entry for today, verify mini-table shows last 7
8. Reports tab: calorie chart renders 30-day line, table below matches, window shift works; weight chart renders on `w` toggle
9. Quit with `q` and `ctrl+c`
10. Relaunch — data persists in SQLite
