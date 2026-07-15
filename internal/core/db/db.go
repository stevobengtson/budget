package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// Open opens a database (SQLite or Postgres) and applies pending
// migrations if shouldMigrate is true. The dsn is interpreted as Postgres if it starts with
// "postgres://" or "postgresql://"; otherwise it's a SQLite file path.
// Use ":memory:" for an ephemeral SQLite DB.
//
// Returns the connection and the detected dialect. Equivalent to
// OpenContext(context.Background(), dsn).
func Open(dsn string, shouldMigrate bool) (*sql.DB, Dialect, error) {
	return OpenContext(context.Background(), dsn, shouldMigrate)
}

// OpenWithTimeout is a convenience wrapper around OpenContext that builds a
// context with the supplied connect deadline. Pass 0 to skip the deadline
// (same as Open, but forces migrations).
func OpenWithTimeout(dsn string, connectTimeout time.Duration) (*sql.DB, Dialect, error) {
	if connectTimeout <= 0 {
		return Open(dsn, true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	return OpenContext(ctx, dsn, true)
}

// OpenContext opens a database and applies pending migrations, honouring
// ctx for the initial connectivity check (PingContext). Migrations
// themselves use the underlying connection and are not subject to the
// caller's context — they're considered part of "opening" the database
// and a partially-applied migration is worse than a slow startup.
func OpenContext(ctx context.Context, dsn string, shouldMigrate bool) (*sql.DB, Dialect, error) {
	if isPostgresDSN(dsn) {
		return openPostgresqlDB(ctx, dsn, shouldMigrate)
	}

	return openSqliteDB(ctx, dsn, shouldMigrate)
}

// isPostgresDSN matches "postgres://" or "postgresql://" URLs.
func isPostgresDSN(s string) bool {
	if strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://") {
		_, err := url.Parse(s)
		return err == nil
	}
	return false
}

func openPostgresqlDB(ctx context.Context, dsn string, shouldMigrate bool) (*sql.DB, Dialect, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, DialectPostgres, fmt.Errorf("open postgres: %w", err)
	}

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, DialectPostgres, fmt.Errorf("ping postgres: %w", err)
	}

	if shouldMigrate {
		if err := migrate(conn, DialectPostgres); err != nil {
			_ = conn.Close()
			return nil, DialectPostgres, err
		}
	}

	return conn, DialectPostgres, nil
}

func openSqliteDB(ctx context.Context, dsn string, shouldMigrate bool) (*sql.DB, Dialect, error) {
	if dsn != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return nil, DialectSQLite, fmt.Errorf("mkdir db dir: %w", err)
		}
	}

	sqliteDSN := dsn + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	if dsn == ":memory:" {
		sqliteDSN = dsn + "?_pragma=foreign_keys(1)"
	}

	conn, err := sql.Open("sqlite", sqliteDSN)
	if err != nil {
		return nil, DialectSQLite, fmt.Errorf("open sqlite: %w", err)
	}

	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, DialectSQLite, fmt.Errorf("ping sqlite: %w", err)
	}

	if shouldMigrate {
		if err := migrate(conn, DialectSQLite); err != nil {
			_ = conn.Close()
			return nil, DialectSQLite, err
		}
	}

	return conn, DialectSQLite, nil
}

func migrate(conn *sql.DB, d Dialect) error {
	gooseDialect := "sqlite3"
	subdir := "migrations/sqlite"
	if d == DialectPostgres {
		gooseDialect = "postgres"
		subdir = "migrations/postgres"
	}
	sub, err := fs.Sub(migrationsFS, subdir)
	if err != nil {
		return fmt.Errorf("migrations subdir: %w", err)
	}
	goose.SetBaseFS(sub)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(conn, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// DefaultPath returns ~/.config/budget/budget.db (respecting XDG_CONFIG_HOME).
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "budget", "budget.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "budget", "budget.db"), nil
}
