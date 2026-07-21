package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenNoMigrate opens a database connection without applying migrations.
// Use this for inspecting or operating on goose state.
func OpenNoMigrate(dsn string) (*sql.DB, Dialect, error) {
	return Open(dsn, false)
}

// configureGoose points goose at the embedded Postgres migrations FS and sets
// the dialect. After this call the caller can invoke any goose function
// (Up, Down, Status, Version, Reset, ...). The Dialect argument is accepted for
// signature stability; the app is Postgres-only.
func configureGoose(_ Dialect) (fs.FS, error) {
	sub, err := fs.Sub(migrationsFS, "migrations/postgres")
	if err != nil {
		return nil, fmt.Errorf("migrations subdir: %w", err)
	}
	goose.SetBaseFS(sub)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, fmt.Errorf("goose dialect: %w", err)
	}
	return sub, nil
}

// MigrateUp applies all pending up migrations.
func MigrateUp(conn *sql.DB, d Dialect) error {
	if _, err := configureGoose(d); err != nil {
		return err
	}
	return goose.Up(conn, ".")
}

// MigrateUpByOne applies a single pending up migration.
func MigrateUpByOne(conn *sql.DB, d Dialect) error {
	if _, err := configureGoose(d); err != nil {
		return err
	}
	return goose.UpByOne(conn, ".")
}

// MigrateDown rolls back the most recently applied migration.
func MigrateDown(conn *sql.DB, d Dialect) error {
	if _, err := configureGoose(d); err != nil {
		return err
	}
	return goose.Down(conn, ".")
}

// MigrateReset rolls all migrations back to zero, then re-applies them.
// Destructive — wipes data. Caller should confirm with the user.
func MigrateReset(conn *sql.DB, d Dialect) error {
	if _, err := configureGoose(d); err != nil {
		return err
	}
	if err := goose.Reset(conn, "."); err != nil {
		return err
	}
	return goose.Up(conn, ".")
}

// MigrateVersion returns the current migration version (0 if none applied).
func MigrateVersion(conn *sql.DB, d Dialect) (int64, error) {
	if _, err := configureGoose(d); err != nil {
		return 0, err
	}
	return goose.GetDBVersion(conn)
}

// MigrateStatus prints the migration state (one row per migration) to
// stdout via goose. The caller chooses where to redirect output.
func MigrateStatus(conn *sql.DB, d Dialect) error {
	if _, err := configureGoose(d); err != nil {
		return err
	}
	// goose.Status writes to its configured log; we re-enable a default
	// stdout-backed logger for this call only.
	prev := goose.NopLogger()
	defer goose.SetLogger(prev)
	goose.SetLogger(stdoutLogger{})
	return goose.Status(conn, ".")
}

// stdoutLogger satisfies goose.Logger by writing to stdout.
type stdoutLogger struct{}

func (stdoutLogger) Printf(format string, v ...any) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	fmt.Printf(format, v...)
}
func (stdoutLogger) Println(v ...any)               { fmt.Println(v...) }
func (stdoutLogger) Fatalf(format string, v ...any) { fmt.Printf(format, v...); os.Exit(1) }
func (stdoutLogger) Fatal(v ...any)                 { fmt.Println(v...); os.Exit(1) }
