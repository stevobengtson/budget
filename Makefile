BINARY  := ./bin/budget
CMD     := ./cmd/budget
MIGRATIONS := ./internal/db/migrations/sqlite

TEMPL ?= templ

.PHONY: build run web tui test clean setup seed db-path db-delete db-migrate db-reset db-status templ tools

setup: tools
	@command -v go >/dev/null 2>&1 || { echo "ERROR: go not found — install from https://go.dev/dl/"; exit 1; }
	go mod download
	go install github.com/pressly/goose/v3/cmd/goose@latest
	@echo "Setup complete. Run 'make run' to start the TUI or 'make web' to serve HTTP."

tools:
	@command -v templ >/dev/null 2>&1 || go install github.com/a-h/templ/cmd/templ@latest

templ: tools
	$(TEMPL) generate ./internal/web

build: templ
	go build -o $(BINARY) $(CMD)

run: build
	$(BINARY) tui

web: build
	$(BINARY) web

tui: run

test: templ
	go test ./...

clean:
	rm -f $(BINARY)

db-path:
	@echo $(DB_PATH)

db-delete:
	rm -f $(DB_PATH) $(DB_PATH)-shm $(DB_PATH)-wal

db-migrate:
	goose -dir $(MIGRATIONS) sqlite3 $(DB_PATH) up

db-reset: db-delete db-migrate

seed: db-migrate build
	$(BINARY) --db $(DB_PATH) db seed

db-status:
	goose -dir $(MIGRATIONS) sqlite3 $(DB_PATH) status
