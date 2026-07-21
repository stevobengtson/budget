package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/postgres/*.sql
var migrationsFS embed.FS

// Open opens the Postgres database at dsn and applies pending migrations when
// shouldMigrate is true. It returns the connection and DialectPostgres — the
// dialect return is retained so the admin/migration helpers keep a stable
// signature. Equivalent to OpenContext(context.Background(), dsn, shouldMigrate).
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

// OpenContext opens the Postgres database and applies pending migrations,
// honouring ctx for the initial connectivity check (PingContext). Migrations
// themselves use the underlying connection and are not subject to the caller's
// context — they're considered part of "opening" the database and a
// partially-applied migration is worse than a slow startup.
func OpenContext(ctx context.Context, dsn string, shouldMigrate bool) (*sql.DB, Dialect, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, DialectPostgres, fmt.Errorf("open postgres: %w", err)
	}

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, DialectPostgres, fmt.Errorf("ping postgres: %w", err)
	}

	if shouldMigrate {
		if err := migrate(conn); err != nil {
			_ = conn.Close()
			return nil, DialectPostgres, err
		}
	}

	return conn, DialectPostgres, nil
}

func migrate(conn *sql.DB) error {
	sub, err := fs.Sub(migrationsFS, "migrations/postgres")
	if err != nil {
		return fmt.Errorf("migrations subdir: %w", err)
	}
	goose.SetBaseFS(sub)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(conn, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
