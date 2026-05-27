# Design: Split web and tui into independently-buildable binaries

Date: 2026-05-27
Status: Approved (pending spec review)

## Problem

Today the project ships a single binary, `cmd/budget`, whose cobra root
(`cmd/budget/cmd`) imports both `internal/tui` (bubbletea) and `internal/web`
(gin + templ). Any build of that binary compiles **both** UI stacks and links
all their dependencies, even though a given deployment only ever runs one of
them. We want to build either client independently, without pulling in the
other's dependencies.

The domain/data layer is already factored into well-bounded packages
(`internal/store`, `internal/db`, `internal/money`, `internal/paydown`,
`internal/config`). The coupling is purely at the binary entrypoint.

## Goals

- Build the web client and the tui client as separate binaries; building one
  must not compile the other's UI dependencies.
- Make the "common, shared by both clients" boundary explicit in the tree.
- Remove genuine cross-client code duplication (presentation helpers).
- Keep every intermediate step compilable and test-green.

## Non-goals

- Splitting into separate Go modules or repos. Single module
  (`github.com/sbengtson/budget`) is retained; Go's per-binary compilation
  already gives lean builds within one module.
- Rewriting domain logic. Moves are mechanical (import-path rewrites).
- Broad refactoring beyond what the split requires.

## Target layout

```
cmd/
  tui/main.go            # shared admin cmds + tui launch (default RunE)
  web/main.go            # shared admin cmds + web launch (default RunE)
  shadcntempl-theme/     # unchanged dev tool
internal/
  core/                  # shared "common by domain" layer
    config/  db/  money/  paydown/  store/   # moved as-is from internal/*
    format/              # NEW: styling-free presenters (goal summary, month labels)
  cli/                   # NEW: shared cobra builders — root flags, config, db, migrate, seed
  tui/                   # TUI client only (imports core)
  web/                   # web client only (imports core)
pkg/shadcntempl/         # unchanged
```

Both binaries are named `budget`; they are separated by output path:
`./bin/tui/budget` and `./bin/web/budget`.

## Components

### 1. Binary split (the decoupling)

Replace the single `cmd/budget` with two `main` packages, `cmd/tui` and
`cmd/web`. Because Go only compiles imported code, `go build ./cmd/tui` never
links gin/templ and `go build ./cmd/web` never links bubbletea. This is the
change that actually delivers independent builds.

Each `main.go`:
1. Builds a cobra root (via `internal/cli`).
2. Registers the shared admin commands (`config`, `db`, `migrate`, `seed`).
3. Adds its own launch command and sets it as the root's default `RunE`
   (preserving today's behavior where bare `budget` launches the TUI; bare
   `web` binary launches the server).

### 2. Shared CLI package (`internal/cli`)

Move the following out of `cmd/budget/cmd` into `internal/cli`, exposed as
constructor functions returning `*cobra.Command` (and the shared config
plumbing):

- Root persistent flags (`--config`, `--db`, `--log-level`) and their viper
  bindings.
- `initConfig` / `resolvedConfig` logic.
- `config`, `db` (up/down/status), `migrate`, `seed` command builders.

These commands only touch `core/db` and `core/store`, so they add no UI deps to
either binary. Both binaries therefore support e.g. `budget db up` and
`budget seed`.

### 3. Formalize the domain layer (`internal/core/*`)

Move `config, db, money, paydown, store` under `internal/core/`. This is a
mechanical import-path rewrite (`internal/store` → `internal/core/store`, etc.)
across the whole tree, applied with sed + `goimports`. No logic changes. The
result makes it obvious at a glance which packages are shared vs. client-private
(`internal/tui`, `internal/web`).

### 4. Extract duplicated presentation (`internal/core/format`)

`internal/tui/budget.go` has `goalSummary(store.CategoryBudget) string`, which
builds the text `"goal $X by Mon YYYY · need $Y/mo"` but bakes in TUI styling
(`styleWarn.Render(...)`). The web client formats the same goal information
separately in its templ/handlers.

Extract the **styling-free** structure into `internal/core/format` (e.g. a
function returning the plain string, or its segments). The TUI re-applies
`styleWarn`; the web client wraps in its own markup. Scope is deliberately
small: `money.Format` already centralizes currency formatting and stays put.
Other genuine duplicates (e.g. month/date labels) will be audited and pulled in
during implementation rather than enumerated speculatively now.

### 5. Build / release / docker

- **Makefile**: add `build-tui` (→ `./bin/tui/budget`, no CSS/templ needed) and
  `build-web` (→ `./bin/web/budget`, requires `css` + `templ`). Keep `build`
  building both. `run` → tui binary, `web` → web binary. `seed`/db targets
  invoke whichever binary is convenient (web or tui both carry the admin cmds).
- **release.yml**: build both binaries per platform; archive names gain a
  `-tui` / `-web` suffix so both ship in a release.
- **Dockerfile**: build only `./cmd/web` → `budget` (the web image needs nothing
  from the tui). Entrypoint stays `budget`, default command `web`'s launch
  (i.e. the web binary launches the server by default, so `CMD` can be empty or
  retain an explicit launch arg).

## Migration order (phased, each step green)

1. **Move domain pkgs** → `internal/core/*`; rewrite imports tree-wide;
   `go build ./... && go test ./...` must pass.
2. **Add `internal/cli`** (extract command builders + config plumbing from
   `cmd/budget/cmd`) and **`internal/core/format`** (extract `goalSummary`
   structure); wire TUI to the new format package. Build + test green.
3. **Add `cmd/web`**; point the web client at the new root; verify
   `go build ./cmd/web` builds standalone and does not link bubbletea.
4. **Add `cmd/tui`**; delete the old `cmd/budget`; update Makefile, release.yml,
   Dockerfile. Verify `go build ./cmd/tui` does not link gin/templ.

## Verification

- `go build ./cmd/tui` and `go build ./cmd/web` both succeed.
- Dependency check: `go list -deps ./cmd/tui` contains no gin/templ;
  `go list -deps ./cmd/web` contains no bubbletea.
- `go test ./...` passes after every phase.
- `make build-tui` and `make build-web` produce `./bin/tui/budget` and
  `./bin/web/budget`; each runs its launch command and `db up` / `seed`.
- Web UI smoke test (server starts, pages render) after the web binary lands.

## Risks / notes

- Import-path churn from the `core/` move is large but mechanical; relying on
  `goimports` + compiler keeps it safe.
- `internal/` import paths mean the `core` packages are still not importable by
  external repos — acceptable, since separate modules/repos are an explicit
  non-goal.
