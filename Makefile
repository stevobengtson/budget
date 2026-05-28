BIN_TUI := ./bin/tui/budget
BIN_WEB := ./bin/web/budget
CMD_TUI := ./cmd/tui
CMD_WEB := ./cmd/web
MIGRATIONS := ./internal/core/db/migrations/sqlite
DB_PATH := ./budget.db

TEMPL    ?= templ
TAILWIND ?= ./bin/tailwindcss

# Tailwind v4 standalone CLI.
TAILWIND_VERSION ?= v4.3.0
UNAME_S := $(shell uname -s | tr '[:upper:]' '[:lower:]')
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_S),darwin)
  TAILWIND_ASSET := tailwindcss-macos-$(if $(filter arm64,$(UNAME_M)),arm64,x64)
else
  TAILWIND_ASSET := tailwindcss-linux-$(if $(filter aarch64 arm64,$(UNAME_M)),arm64,x64)
endif
TAILWIND_URL := https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/$(TAILWIND_ASSET)

CSS_SRC := ./pkg/shadcntempl/tailwind/input.css
CSS_OUT := ./internal/web/static/app.css

.PHONY: build build-tui build-web run web tui test clean setup seed db-path db-delete db-migrate db-reset db-status templ tools tailwind tailwind-watch css theme

setup: tools
	@command -v go >/dev/null 2>&1 || { echo "ERROR: go not found — install from https://go.dev/dl/"; exit 1; }
	go mod download
	go install github.com/pressly/goose/v3/cmd/goose@latest
	@echo "Setup complete. Run 'make run' to start the TUI or 'make web' to serve HTTP."

tools: $(TAILWIND)
	@command -v $(TEMPL) >/dev/null 2>&1 || go install github.com/a-h/templ/cmd/templ@latest

$(TAILWIND):
	@mkdir -p $(dir $(TAILWIND))
	@echo "→ downloading $(TAILWIND_URL)"
	@curl -fsSL -o $(TAILWIND) $(TAILWIND_URL)
	@chmod +x $(TAILWIND)

templ: tools
	$(TEMPL) generate -path ./internal/web
	$(TEMPL) generate -path ./pkg/shadcntempl

css: $(TAILWIND)
	$(TAILWIND) -i $(CSS_SRC) -o $(CSS_OUT) --minify

tailwind: css

tailwind-watch: $(TAILWIND)
	$(TAILWIND) -i $(CSS_SRC) -o $(CSS_OUT) --watch

build-tui:
	go build -o $(BIN_TUI) $(CMD_TUI)

build-web: css templ
	go build -o $(BIN_WEB) $(CMD_WEB)

build: build-tui build-web

run: build-tui
	$(BIN_TUI)

web: build-web
	$(BIN_WEB)

dev:
	air

tui: run

test: templ
	go test ./...

# Fetch a shadcn theme preset and overwrite pkg/shadcntempl/theme/theme.css.
# Usage: make theme PRESET=b6FTKD8F6
#    or: make theme URL=https://ui.shadcn.com/r/themes/xyz.json
theme:
	go run ./cmd/shadcntempl-theme -out ./pkg/shadcntempl/theme/theme.css $(if $(URL),-url $(URL),)$(if $(PRESET), -preset $(PRESET),)

clean:
	rm -rf ./bin/tui ./bin/web $(CSS_OUT)

db-path:
	@echo $(DB_PATH)

db-delete:
	rm -f $(DB_PATH) $(DB_PATH)-shm $(DB_PATH)-wal

db-migrate:
	goose -dir $(MIGRATIONS) sqlite3 $(DB_PATH) up

db-reset: db-delete db-migrate

seed: db-migrate build-tui
	$(BIN_TUI) --db $(DB_PATH) db seed

db-status:
	goose -dir $(MIGRATIONS) sqlite3 $(DB_PATH) status
