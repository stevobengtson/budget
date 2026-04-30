BINARY  := ./bin/budget
CMD     := ./cmd/budget
DB_PATH := ./data/budget.db
MIGRATIONS := ./internal/db/migrations

.PHONY: build run test clean setup db-path db-delete db-migrate db-reset db-status

setup:
	@command -v go >/dev/null 2>&1 || { echo "ERROR: go not found — install from https://go.dev/dl/"; exit 1; }
	go mod download
	go install github.com/pressly/goose/v3/cmd/goose@latest
	@echo "Setup complete. Run 'make run' to start."

build:
	go build -o $(BINARY) $(CMD)

run:
	go run $(CMD) --db $(DB_PATH)

test:
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

db-status:
	goose -dir $(MIGRATIONS) sqlite3 $(DB_PATH) status
