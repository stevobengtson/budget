# Split web/tui into independently-buildable binaries — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the web and tui clients as two separate binaries (`./bin/web/budget`, `./bin/tui/budget`) so that compiling one never links the other's UI dependencies.

**Architecture:** Single Go module retained. Domain packages move under `internal/core/*`. Cobra command builders (root flags, config/db/migrate/seed) move into a shared `internal/cli` package exposed as methods on an `App` struct. Two thin `main` packages (`cmd/web`, `cmd/tui`) each build a root, register the shared admin commands, and add their own UI launch command. Duplicated goal-formatting logic moves into `internal/core/format`.

**Tech Stack:** Go 1.26, cobra, viper, bubbletea (tui), gin + templ (web), goose migrations, modernc sqlite / pgx.

**Spec:** `docs/superpowers/specs/2026-05-27-split-web-tui-binaries-design.md`

---

## File structure (end state)

```
cmd/
  tui/main.go            # NEW — tui binary entrypoint
  web/main.go            # NEW — web binary entrypoint
  shadcntempl-theme/     # unchanged
  budget/                # DELETED
internal/
  core/                  # shared domain layer
    config/  db/  money/  paydown/  store/   # MOVED from internal/*
    format/format.go     # NEW — goal-summary formatting
    format/format_test.go
  cli/                   # NEW — shared cobra builders
    cli.go               # App struct, Root(), config plumbing, persistent flags
    config.go            # (*App).ConfigCmd()
    db.go                # (*App).DBCmd() + openForAdmin
    migrate.go           # (*App).MigrateCmd()
    seed.go              # (*App).SeedCmd() + seed data (verbatim move)
  tui/                   # unchanged code, import paths rewritten
  web/                   # unchanged code, import paths rewritten
pkg/shadcntempl/         # unchanged
```

---

## Task 1: Move domain packages under internal/core

**Files:**
- Move: `internal/{config,db,money,paydown,store}` → `internal/core/{config,db,money,paydown,store}`
- Modify (mechanical import rewrite): every `*.go` file importing those packages
- Modify: `Makefile:3`

- [ ] **Step 1: Move the five packages with git mv**

```bash
mkdir -p internal/core
git mv internal/config internal/core/config
git mv internal/db internal/core/db
git mv internal/money internal/core/money
git mv internal/paydown internal/core/paydown
git mv internal/store internal/core/store
```

- [ ] **Step 2: Rewrite import paths across all Go files**

```bash
grep -rl 'sbengtson/budget/internal/\(config\|db\|money\|paydown\|store\)' --include='*.go' . \
  | xargs sed -i '' \
      -e 's#sbengtson/budget/internal/config#sbengtson/budget/internal/core/config#g' \
      -e 's#sbengtson/budget/internal/db#sbengtson/budget/internal/core/db#g' \
      -e 's#sbengtson/budget/internal/money#sbengtson/budget/internal/core/money#g' \
      -e 's#sbengtson/budget/internal/paydown#sbengtson/budget/internal/core/paydown#g' \
      -e 's#sbengtson/budget/internal/store#sbengtson/budget/internal/core/store#g'
```

Note: `sed -i ''` is the BSD/macOS form (empty backup suffix). The migrations directory (`go:embed`) moved with the `db` package, so the embed still resolves.

- [ ] **Step 3: Update the Makefile migrations path**

In `Makefile` line 3, change:

```makefile
MIGRATIONS := ./internal/db/migrations/sqlite
```
to:
```makefile
MIGRATIONS := ./internal/core/db/migrations/sqlite
```

- [ ] **Step 4: Regenerate templ (templ files import store) and tidy imports**

```bash
templ generate -path ./internal/web
templ generate -path ./pkg/shadcntempl
gofmt -w ./internal ./cmd ./pkg
```

- [ ] **Step 5: Verify build and tests pass**

Run: `go build ./... && go test ./...`
Expected: PASS (no logic changed — pure path move).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: move domain packages under internal/core"
```

---

## Task 2: Create internal/core/format goal-summary package

**Files:**
- Create: `internal/core/format/format.go`
- Test: `internal/core/format/format_test.go`

This package centralizes the goal wording duplicated by the TUI (`internal/tui/budget.go` `goalSummary`) and the web view (`internal/web/views/budget.templ` `GoalCell`): the `Jan 2006` date layout, the `/mo` suffix, and currency formatting via `money.Format`.

- [ ] **Step 1: Write the failing test**

`internal/core/format/format_test.go`:

```go
package format

import (
	"testing"
	"time"
)

func cents(c int64) *int64 { return &c }

func TestGoalForNoGoal(t *testing.T) {
	if _, ok := GoalFor(nil, nil, 0); ok {
		t.Fatal("expected ok=false when goalCents is nil")
	}
}

func TestGoalForAmountOnly(t *testing.T) {
	g, ok := GoalFor(cents(185000), nil, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if g.Amount != "$1,850.00" {
		t.Fatalf("Amount = %q, want $1,850.00", g.Amount)
	}
	if g.Due != "" || g.Need != "" {
		t.Fatalf("Due/Need should be empty, got %q / %q", g.Due, g.Need)
	}
}

func TestGoalForFull(t *testing.T) {
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	g, ok := GoalFor(cents(300000), &due, 15000)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if g.Due != "Sep 2026" {
		t.Fatalf("Due = %q, want Sep 2026", g.Due)
	}
	if g.Need != "$150.00/mo" {
		t.Fatalf("Need = %q, want $150.00/mo", g.Need)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/core/format/...`
Expected: FAIL (package/function does not compile — `GoalFor` undefined).

- [ ] **Step 3: Implement the package**

`internal/core/format/format.go`:

```go
// Package format holds presentation helpers shared by the tui and web
// clients so wording (goal summaries, date labels) stays consistent.
package format

import (
	"time"

	"github.com/sbengtson/budget/internal/core/money"
)

// GoalDateLayout is the month/year layout used when displaying a goal due date.
const GoalDateLayout = "Jan 2006"

// Goal holds the formatted pieces of a category goal. Callers assemble them
// however their medium requires (plain text for the tui, HTML spans for web).
type Goal struct {
	Amount string // formatted goal amount, e.g. "$1,850.00"
	Due    string // formatted due date ("Jan 2006"), empty when none
	Need   string // formatted monthly need, e.g. "$150.00/mo", empty when target <= 0
}

// GoalFor builds the formatted goal pieces. ok is false when no goal is set.
func GoalFor(goalCents *int64, due *time.Time, monthlyTarget int64) (g Goal, ok bool) {
	if goalCents == nil {
		return Goal{}, false
	}
	g.Amount = money.Format(*goalCents)
	if due != nil {
		g.Due = due.Format(GoalDateLayout)
	}
	if monthlyTarget > 0 {
		g.Need = money.Format(monthlyTarget) + "/mo"
	}
	return g, true
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/core/format/...`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/format
git commit -m "feat: add core/format package for goal summaries"
```

---

## Task 3: Refactor TUI goalSummary to use core/format

**Files:**
- Modify: `internal/tui/budget.go:384-396` (the `goalSummary` function)

- [ ] **Step 1: Add the format import**

In `internal/tui/budget.go`, add to the import block:

```go
"github.com/sbengtson/budget/internal/core/format"
```

- [ ] **Step 2: Replace the goalSummary body**

Replace the existing function (currently lines 381-396) with:

```go
// goalSummary formats a category's goal as "goal $X by Mon YYYY · need $Y/mo".
// Returns empty string when no goal is set. Shared by the budget table and the
// assign/goal forms so wording stays consistent.
func goalSummary(r store.CategoryBudget) string {
	g, ok := format.GoalFor(r.GoalCents, r.GoalDueDate, r.MonthlyTarget)
	if !ok {
		return ""
	}
	out := "goal " + g.Amount
	if g.Due != "" {
		out += " by " + g.Due
	}
	if g.Need != "" {
		out += styleWarn.Render(" · need " + g.Need)
	}
	return out
}
```

- [ ] **Step 3: Verify build and existing tests pass**

Run: `go build ./... && go test ./internal/tui/...`
Expected: PASS. (Output string is byte-identical to before — `money.Format` and the `Jan 2006` layout are unchanged.)

- [ ] **Step 4: Commit**

```bash
git add internal/tui/budget.go
git commit -m "refactor: tui goalSummary uses core/format"
```

---

## Task 4: Refactor web GoalCell to use core/format

**Files:**
- Modify: `internal/web/views/budget.templ:213-227` (the `GoalCell` templ)
- Regenerate: `internal/web/views/budget_templ.go`

- [ ] **Step 1: Inspect the current GoalCell**

Run: `sed -n '213,230p' internal/web/views/budget.templ`
Expected: a `templ GoalCell(r store.CategoryBudget)` that formats `money.Format(*r.GoalCents)`, `r.GoalDueDate.Format("Jan 2006")`, and `money.Format(r.MonthlyTarget)` followed by a literal `/mo`.

- [ ] **Step 2: Rewrite GoalCell to use format.GoalFor**

Replace the `GoalCell` templ definition with the version below. Keep the surrounding HTML/classes exactly as they already are in the file — only swap the value expressions to read from `format.GoalFor`. Update the templ import block to add `"github.com/sbengtson/budget/internal/core/format"` and drop the `money` import if it becomes unused in this file.

```go
templ GoalCell(r store.CategoryBudget) {
	if g, ok := format.GoalFor(r.GoalCents, r.GoalDueDate, r.MonthlyTarget); ok {
		<div class="flex flex-col">
			<span>goal { g.Amount }</span>
			if g.Due != "" {
				<span class="text-xs text-muted-foreground">by { g.Due }</span>
			}
			if g.Need != "" {
				<span class="text-xs text-muted-foreground">need { g.Need }</span>
			}
		</div>
	}
}
```

Note: previously the template rendered `need { money.Format(r.MonthlyTarget) }/mo`; now `g.Need` already includes the `/mo` suffix, so do not append a literal `/mo`.

- [ ] **Step 3: Regenerate templ**

Run: `templ generate -path ./internal/web`
Expected: `internal/web/views/budget_templ.go` regenerated with no errors.

- [ ] **Step 4: Verify build and web tests pass**

Run: `go build ./... && go test ./internal/web/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/views/budget.templ internal/web/views/budget_templ.go
git commit -m "refactor: web GoalCell uses core/format"
```

---

## Task 5: Create shared internal/cli package

This extracts the cobra plumbing from `cmd/budget/cmd` into reusable builders on an `App` struct. Package-level globals (`rootCmd`, `v`, `cfgFile`, `resolvedConfig`) become fields/methods. The UI launch commands are NOT moved here — they stay in each binary's `main` package so `internal/cli` imports only `core/*` (no bubbletea, no gin).

**Files:**
- Create: `internal/cli/cli.go`
- Create: `internal/cli/config.go`
- Create: `internal/cli/db.go`
- Create: `internal/cli/migrate.go`
- Create: `internal/cli/seed.go`

- [ ] **Step 1: Create internal/cli/cli.go (App + root + config plumbing)**

```go
// Package cli holds cobra command builders shared by the tui and web
// binaries: root flags, config resolution, and the db/migrate/seed admin
// commands. It imports only internal/core packages, never a UI package, so
// linking it adds no UI dependencies to either binary.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/config"
)

// App carries shared CLI state (config file path + viper instance) and builds
// cobra commands wired to it.
type App struct {
	cfgFile string
	v       *viper.Viper
}

// NewApp constructs an App with a fresh viper instance.
func NewApp() *App {
	return &App{v: viper.New()}
}

// Root builds the root command with persistent flags and config initialization.
// The caller adds subcommands and sets a default RunE (its UI launch).
func (a *App) Root(use, short, long string) *cobra.Command {
	root := &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         long,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&a.cfgFile, "config", "",
		"config file (default: ./budget.yaml or $XDG_CONFIG_HOME/budget/config.yaml)")
	root.PersistentFlags().String("db", "",
		"database DSN (SQLite path or postgres://...). Overrides config and env.")
	root.PersistentFlags().String("log-level", "",
		"log level (debug|info|warn|error)")

	cobra.CheckErr(a.v.BindPFlag("db.dsn", root.PersistentFlags().Lookup("db")))
	cobra.CheckErr(a.v.BindPFlag("log.level", root.PersistentFlags().Lookup("log-level")))

	cobra.OnInitialize(a.initConfig)
	return root
}

func (a *App) initConfig() {
	if a.cfgFile != "" {
		a.v.SetConfigFile(a.cfgFile)
	} else {
		for _, p := range config.DefaultConfigSearchPaths() {
			a.v.AddConfigPath(p)
		}
		a.v.SetConfigName("budget")
		a.v.SetConfigType("yaml")
		_ = a.v.MergeInConfig()
		a.v.SetConfigName("config")
	}
	if err := a.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	}
}

// ResolvedConfig returns the fully-resolved configuration.
func (a *App) ResolvedConfig() (config.Config, error) {
	return config.Load(a.v)
}
```

- [ ] **Step 2: Create internal/cli/config.go**

Port `cmd/budget/cmd/config.go` to a builder. `v.ConfigFileUsed()` becomes `a.v.ConfigFileUsed()`.

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ConfigCmd builds the `config` command group (show / path).
func (a *App) ConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect resolved configuration",
	}

	configShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Print the resolved config (flags > env > file > defaults)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.ResolvedConfig()
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}

	configPathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the path of the loaded config file (or empty if none)",
		RunE: func(cmd *cobra.Command, args []string) error {
			used := a.v.ConfigFileUsed()
			if used == "" {
				fmt.Fprintln(os.Stderr, "(no config file loaded)")
				return nil
			}
			fmt.Println(used)
			return nil
		},
	}

	configCmd.AddCommand(configShowCmd, configPathCmd)
	return configCmd
}
```

- [ ] **Step 3: Create internal/cli/db.go**

Port `cmd/budget/cmd/db.go`. `openForAdmin` becomes a method; the `seed` subcommand is attached here (it was registered under `dbCmd` originally) by calling `a.seedCmd()`.

```go
package cli

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/core/db"
)

// DBCmd builds the `db` command group (up / up-one / down / reset / status /
// version) plus the `seed` subcommand.
func (a *App) DBCmd() *cobra.Command {
	dbCmd := &cobra.Command{
		Use:   "db",
		Short: "Schema migrations (apply / rollback / status)",
		Long: `Manage the database schema using the embedded goose migrations.

These commands operate on the database identified by the resolved
config (--db flag, BUDGET_DB_DSN env var, or budget.yaml). To target a
different database temporarily, pass --db on the command line.`,
	}

	mkVersionPrinter := func(run func(*sql.DB, db.Dialect) error) func(*cobra.Command, []string) error {
		return func(c *cobra.Command, args []string) error {
			conn, dialect, err := a.openForAdmin()
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			if err := run(conn, dialect); err != nil {
				return err
			}
			v, _ := db.MigrateVersion(conn, dialect)
			fmt.Printf("ok — version %d\n", v)
			return nil
		}
	}

	dbUpCmd := &cobra.Command{Use: "up", Short: "Apply all pending migrations",
		RunE: mkVersionPrinter(db.MigrateUp)}
	dbUpOneCmd := &cobra.Command{Use: "up-one", Short: "Apply the next pending migration",
		RunE: mkVersionPrinter(db.MigrateUpByOne)}
	dbDownCmd := &cobra.Command{Use: "down", Short: "Roll back the most recently applied migration",
		RunE: mkVersionPrinter(db.MigrateDown)}
	dbResetCmd := &cobra.Command{Use: "reset",
		Short: "Roll back to zero and re-apply all migrations (DESTRUCTIVE: wipes data)",
		RunE:  mkVersionPrinter(db.MigrateReset)}

	dbStatusCmd := &cobra.Command{Use: "status", Short: "Print one line per migration (applied / pending)",
		RunE: func(c *cobra.Command, args []string) error {
			conn, dialect, err := a.openForAdmin()
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			return db.MigrateStatus(conn, dialect)
		}}

	dbVersionCmd := &cobra.Command{Use: "version", Short: "Print the current migration version",
		RunE: func(c *cobra.Command, args []string) error {
			conn, dialect, err := a.openForAdmin()
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			v, err := db.MigrateVersion(conn, dialect)
			if err != nil {
				return err
			}
			fmt.Println(v)
			return nil
		}}

	dbCmd.AddCommand(dbUpCmd, dbUpOneCmd, dbDownCmd, dbResetCmd, dbStatusCmd, dbVersionCmd, a.seedCmd())
	return dbCmd
}

func (a *App) openForAdmin() (*sql.DB, db.Dialect, error) {
	cfg, err := a.ResolvedConfig()
	if err != nil {
		return nil, 0, err
	}
	conn, dialect, err := db.OpenNoMigrate(cfg.DB.DSN)
	if err != nil {
		return nil, 0, fmt.Errorf("open db: %w", err)
	}
	return conn, dialect, nil
}
```

Note: confirm `db.MigrateUp`, `MigrateUpByOne`, `MigrateDown`, `MigrateReset` all have signature `func(*sql.DB, db.Dialect) error` (they do in `internal/core/db/admin.go`). If any differs, fall back to an inline RunE for that one rather than `mkVersionPrinter`.

- [ ] **Step 4: Create internal/cli/migrate.go**

Port `cmd/budget/cmd/migrate.go`. The `--from`/`--to` flags are bound to local vars captured in the closure.

```go
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/core/db"
)

// MigrateCmd builds the `migrate` command (copy all data between two DBs).
func (a *App) MigrateCmd() *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Copy all data between two databases (SQLite ↔ Postgres)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if from == "" || to == "" {
				return fmt.Errorf("both --from and --to are required")
			}
			ctx := context.Background()
			src, srcDialect, err := db.Open(from)
			if err != nil {
				return fmt.Errorf("open source: %w", err)
			}
			defer func() { _ = src.Close() }()
			dst, dstDialect, err := db.Open(to)
			if err != nil {
				return fmt.Errorf("open dest: %w", err)
			}
			defer func() { _ = dst.Close() }()

			fmt.Fprintf(os.Stderr, "migrating %s → %s ...\n", srcDialect, dstDialect)
			if err := db.CopyAll(ctx, src, srcDialect, dst, dstDialect); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "done.")
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source DSN (SQLite path or postgres URL)")
	cmd.Flags().StringVar(&to, "to", "", "destination DSN")
	return cmd
}
```

- [ ] **Step 5: Create internal/cli/seed.go**

Move `cmd/budget/cmd/seed.go` here. Change `package cmd` → `package cli`, rewrite the store/db imports to `internal/core/...`, wrap the command body in `func (a *App) seedCmd() *cobra.Command { ... }`, and replace `resolvedConfig()` with `a.ResolvedConfig()`. The `seed(ctx, s)` function and all helpers (`ptr64`, `ptrStr`, `ptrTime`, `cents`, `day`, and the entire seed body — ~380 lines) move verbatim; only the package clause and import paths change.

```bash
git mv cmd/budget/cmd/seed.go internal/cli/seed.go
```

Then edit `internal/cli/seed.go`:
- Line 1: `package cmd` → `package cli`
- Imports: `internal/store` → `internal/core/store`, `internal/db` → `internal/core/db`
- Replace the `var seedCmd = &cobra.Command{...}` block + `func init() { dbCmd.AddCommand(seedCmd) }` with a method that returns the command:

```go
// seedCmd builds the `seed` command (registered under `db` by DBCmd).
func (a *App) seedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Populate the database with three months of demo data",
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := a.ResolvedConfig()
			if err != nil {
				return err
			}
			conn, dialect, err := db.Open(cfg.DB.DSN)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer func() { _ = conn.Close() }()

			sd := store.DialectSQLite
			if dialect == db.DialectPostgres {
				sd = store.DialectPostgres
			}
			s := store.NewWithDialect(conn, sd)
			ctx := context.Background()

			groups, _ := s.ListGroups(ctx)
			for _, g := range groups {
				if g.Name != "Income" {
					return fmt.Errorf("database already has data — wipe it first")
				}
			}
			if err := seed(ctx, s); err != nil {
				return err
			}
			fmt.Println("seeded successfully")
			return nil
		},
	}
}
```

Leave `func seed(ctx context.Context, s *store.Store) error { ... }` and the `ptr64`/`ptrStr`/`ptrTime`/`cents`/`day` helpers exactly as they were.

- [ ] **Step 6: Verify the cli package builds**

Run: `go build ./internal/cli/...`
Expected: PASS. (Note: `cmd/budget` will NOT build yet — it still references the now-removed `seed.go`. That is fixed in Tasks 6–7. Build only the cli package here.)

- [ ] **Step 7: Commit**

```bash
git add internal/cli
git commit -m "feat: add shared internal/cli command builders"
```

---

## Task 6: Create the web binary (cmd/web)

**Files:**
- Create: `cmd/web/main.go`

- [ ] **Step 1: Write cmd/web/main.go**

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/cli"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web"
)

func main() {
	app := cli.NewApp()
	root := app.Root("budget",
		"Personal budget — web server",
		`budget (web) serves the HTMX + Templ web UI.

Configuration is read from budget.yaml, BUDGET_* env vars, and CLI flags.
The db/migrate/seed/config admin commands are available as subcommands.`)

	var addr string
	launch := func() error {
		cfg, err := app.ResolvedConfig()
		if err != nil {
			return err
		}
		a := addr
		if a == "" {
			a = cfg.Web.Addr
		}
		conn, dialect, err := db.Open(cfg.DB.DSN)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer func() { _ = conn.Close() }()

		sd := store.DialectSQLite
		if dialect == db.DialectPostgres {
			sd = store.DialectPostgres
		}
		s := store.NewWithDialect(conn, sd)

		srv := web.NewServer(s)
		fmt.Printf("budget web — listening on http://localhost%s (db=%s)\n", a, cfg.DB.DSN)
		return http.ListenAndServe(a, srv.Handler())
	}

	webCmd := &cobra.Command{
		Use:   "web",
		Short: "Launch the HTTP server (HTMX + Templ)",
		RunE:  func(c *cobra.Command, args []string) error { return launch() },
	}
	webCmd.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")

	// Bare `budget` (web binary) launches the server.
	root.RunE = func(c *cobra.Command, args []string) error { return launch() }
	root.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")

	root.AddCommand(webCmd, app.ConfigCmd(), app.DBCmd(), app.MigrateCmd())
	cobra.CheckErr(root.Execute())
}
```

Note: `addr` is shared by both the root default RunE and the `web` subcommand; registering the flag on both keeps `budget --addr ...` and `budget web --addr ...` working.

- [ ] **Step 2: Verify the web binary builds and excludes bubbletea**

Run:
```bash
go build ./cmd/web
go list -deps ./cmd/web | grep -i bubbletea || echo "OK: no bubbletea in web binary"
```
Expected: build succeeds; the grep prints `OK: no bubbletea in web binary`.

- [ ] **Step 3: Commit**

```bash
git add cmd/web
git commit -m "feat: add standalone web binary (cmd/web)"
```

---

## Task 7: Create the tui binary and delete cmd/budget

**Files:**
- Create: `cmd/tui/main.go`
- Delete: `cmd/budget/` (entire directory)

- [ ] **Step 1: Write cmd/tui/main.go**

```go
package main

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/cli"
	"github.com/sbengtson/budget/internal/tui"
)

func main() {
	app := cli.NewApp()
	root := app.Root("budget",
		"Personal budget — terminal UI",
		`budget (tui) launches the keyboard-driven terminal UI.

Configuration is read from budget.yaml, BUDGET_* env vars, and CLI flags.
The db/migrate/seed/config admin commands are available as subcommands.`)

	launch := func() error {
		cfg, err := app.ResolvedConfig()
		if err != nil {
			return err
		}
		zone.NewGlobal()
		boot := tui.NewBootstrap(cfg.DB.DSN, 0)
		defer boot.Close()
		if _, err := tea.NewProgram(boot, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
			return err
		}
		return nil
	}

	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the terminal UI",
		RunE:  func(c *cobra.Command, args []string) error { return launch() },
	}

	// Bare `budget` (tui binary) launches the TUI.
	root.RunE = func(c *cobra.Command, args []string) error { return launch() }

	root.AddCommand(tuiCmd, app.ConfigCmd(), app.DBCmd(), app.MigrateCmd())
	cobra.CheckErr(root.Execute())
}
```

- [ ] **Step 2: Delete the old single binary**

```bash
git rm -r cmd/budget
```

- [ ] **Step 3: Verify the tui binary builds and excludes gin**

Run:
```bash
go build ./cmd/tui
go list -deps ./cmd/tui | grep -i 'gin-gonic\|a-h/templ' || echo "OK: no gin/templ in tui binary"
```
Expected: build succeeds; the grep prints `OK: no gin/templ in tui binary`.

- [ ] **Step 4: Verify the whole module builds and tests pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add standalone tui binary, remove cmd/budget"
```

---

## Task 8: Update the Makefile for two binaries

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Replace binary/build variables and targets**

Change the top variables:

```makefile
BINARY  := ./bin/budget
CMD     := ./cmd/budget
```
to:
```makefile
BIN_TUI := ./bin/tui/budget
BIN_WEB := ./bin/web/budget
CMD_TUI := ./cmd/tui
CMD_WEB := ./cmd/web
```

- [ ] **Step 2: Update the .PHONY line and build/run/web targets**

Update `.PHONY` to include `build-tui build-web` and replace the `build`, `run`, `web`, `seed`, `clean` targets:

```makefile
.PHONY: build build-tui build-web run web test clean setup seed db-path db-delete db-migrate db-reset db-status templ tools tailwind tailwind-watch css theme

build-tui:
	go build -o $(BIN_TUI) $(CMD_TUI)

build-web: css templ
	go build -o $(BIN_WEB) $(CMD_WEB)

build: build-tui build-web

run: build-tui
	$(BIN_TUI)

web: build-web
	$(BIN_WEB)

tui: run

seed: db-migrate build-tui
	$(BIN_TUI) --db $(DB_PATH) db seed

clean:
	rm -rf ./bin/tui ./bin/web $(CSS_OUT)
```

Note: the tui binary needs no CSS/templ, so `build-tui` has no prerequisites; `build-web` keeps `css templ`. `run` launches the tui binary with no subcommand (bare launch).

- [ ] **Step 3: Verify the Makefile targets work**

Run: `make build-tui && make build-web`
Expected: produces `./bin/tui/budget` and `./bin/web/budget`.

- [ ] **Step 4: Smoke-test the binaries**

Run:
```bash
./bin/web/budget config show
./bin/tui/budget config show
```
Expected: both print the resolved JSON config (proves shared admin commands work in both binaries).

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "build: split Makefile into build-tui and build-web"
```

---

## Task 9: Update the release workflow to build both binaries

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Replace the "Build binary" step**

Replace the single build step (currently building `./cmd/budget` to `budget${EXT}`) with one that builds both binaries to distinct names:

```yaml
      - name: Build binaries
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          CGO_ENABLED: "0"
        run: |
          EXT=""
          if [ "$GOOS" = "windows" ]; then EXT=".exe"; fi
          go build -trimpath -ldflags="-s -w" -o "budget-tui${EXT}" ./cmd/tui
          go build -trimpath -ldflags="-s -w" -o "budget-web${EXT}" ./cmd/web
```

Note: the web binary needs `internal/web/static/app.css` (embedded). Add a CSS build step before "Build binaries" so the embed is populated — mirror the Dockerfile approach:

```yaml
      - name: Build web assets
        run: |
          curl -fsSL -o /usr/local/bin/tailwindcss \
            "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.0/tailwindcss-linux-x64"
          chmod +x /usr/local/bin/tailwindcss
          /usr/local/bin/tailwindcss -i ./pkg/shadcntempl/tailwind/input.css -o ./internal/web/static/app.css --minify
          go install github.com/a-h/templ/cmd/templ@v0.3.1001
          $(go env GOPATH)/bin/templ generate -path ./pkg/shadcntempl
          $(go env GOPATH)/bin/templ generate -path ./internal/web
```

(The release runner is `ubuntu-latest`, so the linux-x64 tailwind asset is correct regardless of the target GOARCH — asset generation is host-arch, cross-compilation is Go-only.)

- [ ] **Step 2: Update the tar packaging step**

In the tar packaging step, replace `mv budget "$NAME/"` with both binaries:

```bash
          mv budget-tui budget-web "$NAME/"
```

- [ ] **Step 3: Update the zip packaging step**

In the zip packaging step, replace `mv budget.exe "$NAME/"` with:

```bash
          mv budget-tui.exe budget-web.exe "$NAME/"
```

- [ ] **Step 4: Validate the workflow YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))" && echo OK`
Expected: `OK` (well-formed YAML). CI itself only runs on tag push; this local check is the verification available here.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: build and package both tui and web binaries"
```

---

## Task 10: Update the Dockerfile to build the web binary

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Point the go build at cmd/web**

In `Dockerfile` line 46, change:

```dockerfile
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/budget ./cmd/budget
```
to:
```dockerfile
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/budget ./cmd/web
```

The runtime stage (`COPY --from=builder /out/budget`, `ENTRYPOINT ["budget"]`, `CMD ["web"]`) is unchanged: the web binary still accepts the `web` subcommand and the admin subcommands.

- [ ] **Step 2: Verify the build command resolves (dry sanity check)**

Run: `go build -o /tmp/budget-web-dockercheck ./cmd/web && echo OK`
Expected: `OK` (confirms the path the Dockerfile now uses builds locally). Building the actual image is optional and environment-dependent.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "build: docker image builds the web binary"
```

---

## Task 11: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full build + test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 2: Confirm dependency isolation both directions**

Run:
```bash
go list -deps ./cmd/tui | grep -i 'gin-gonic\|a-h/templ' && echo "FAIL: tui links web deps" || echo "OK tui clean"
go list -deps ./cmd/web | grep -i 'bubbletea\|bubblezone' && echo "FAIL: web links tui deps" || echo "OK web clean"
```
Expected: `OK tui clean` and `OK web clean`.

- [ ] **Step 3: Confirm both binaries carry the admin commands**

Run:
```bash
./bin/tui/budget db version
./bin/web/budget db version
```
Expected: both print a migration version number (after `make build-tui build-web`).

- [ ] **Step 4: Update README references (if any) to the new binary paths**

Run: `grep -rn 'cmd/budget\|bin/budget\|budget tui\|budget web' README.md`
For each hit, update to the new layout (`./bin/tui/budget`, `./bin/web/budget`, `go build ./cmd/tui`, `go build ./cmd/web`). If there are no hits, skip.

- [ ] **Step 5: Commit any README changes**

```bash
git add README.md
git commit -m "docs: update README for split tui/web binaries"
```

---

## Self-review notes

- **Spec coverage:** binary split (Tasks 6,7), shared cli incl. admin cmds in both (Task 5, verified Task 11.3), core/* move (Task 1), core/format extraction + both clients adopting it (Tasks 2,3,4), build/release/docker (Tasks 8,9,10), dependency-isolation verification (Tasks 6.2, 7.3, 11.2). All spec sections mapped.
- **Naming consistency:** `App`, `NewApp`, `Root`, `ResolvedConfig`, `ConfigCmd`, `DBCmd`, `MigrateCmd`, `seedCmd`, `openForAdmin`, `format.GoalFor`, `format.Goal{Amount,Due,Need}`, `format.GoalDateLayout` used identically across tasks.
- **Ordering:** core move first (keeps tree green), then format + adopters, then cli, then binaries, then build tooling, then final checks. `cmd/budget` is intentionally left non-building between Task 5 and Task 7 (Task 5 step 6 builds only the cli package); it is removed in Task 7.
