BIN_TUI := ./bin/tui/budget
BIN_API := ./bin/api/budget
CMD_TUI := ./cmd/tui
CMD_API := ./cmd/api
MIGRATIONS := ./internal/core/db/migrations/sqlite
DB_PATH := ./budget.db

# Postgres API database (multi-user). Override API_DSN as needed.
MIGRATIONS_PG := ./internal/core/db/migrations/postgres
API_DSN ?= postgres://postgres:postgres@localhost:5432/budget_api?sslmode=disable

.PHONY: build build-tui build-api run tui api dev test clean setup seed \
        db-path db-delete db-migrate db-reset db-status migrate-api migrate-api-status

setup:
	@command -v go >/dev/null 2>&1 || { echo "ERROR: go not found — install from https://go.dev/dl/"; exit 1; }
	go mod download
	go install github.com/pressly/goose/v3/cmd/goose@latest
	@echo "Setup complete. Run 'make run' for the TUI or 'make api' for the JSON API."

build-tui:
	go build -o $(BIN_TUI) $(CMD_TUI)

build-api:
	go build -o $(BIN_API) $(CMD_API)

build: build-tui build-api

run: build-tui
	$(BIN_TUI)

tui: run

api: build-api
	$(BIN_API)

dev:
	air

test:
	go test ./...

clean:
	rm -rf ./bin/tui ./bin/api

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

# Postgres API DB (budget_api) — multi-user store behind the JSON API.
migrate-api:
	goose -dir $(MIGRATIONS_PG) postgres "$(API_DSN)" up

migrate-api-status:
	goose -dir $(MIGRATIONS_PG) postgres "$(API_DSN)" status
