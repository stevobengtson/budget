package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// OpenNoMigrate opens a database connection without applying migrations.
// Use this for inspecting or operating on goose state.
func OpenNoMigrate(dsn string) (*sql.DB, Dialect, error) {
	if isPostgresDSN(dsn) {
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, DialectPostgres, fmt.Errorf("open postgres: %w", err)
		}
		if err := conn.Ping(); err != nil {
			_ = conn.Close()
			return nil, DialectPostgres, fmt.Errorf("ping postgres: %w", err)
		}
		return conn, DialectPostgres, nil
	}
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
	if err := conn.Ping(); err != nil {
		return nil, DialectSQLite, fmt.Errorf("ping sqlite: %w", err)
	}
	return conn, DialectSQLite, nil
}

// configureGoose points goose at the embedded migrations FS and sets the
// dialect. After this call the caller can invoke any goose function
// (Up, Down, Status, Version, Reset, ...).
func configureGoose(d Dialect) (fs.FS, error) {
	gooseDialect := "sqlite3"
	subdir := "migrations/sqlite"
	if d == DialectPostgres {
		gooseDialect = "postgres"
		subdir = "migrations/postgres"
	}
	sub, err := fs.Sub(migrationsFS, subdir)
	if err != nil {
		return nil, fmt.Errorf("migrations subdir: %w", err)
	}
	goose.SetBaseFS(sub)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect(gooseDialect); err != nil {
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
	if !strings.HasSuffix(format, "\n") { format += "\n" }
	fmt.Printf(format, v...)
}
func (stdoutLogger) Println(v ...any)               { fmt.Println(v...) }
func (stdoutLogger) Fatalf(format string, v ...any) { fmt.Printf(format, v...); os.Exit(1) }
func (stdoutLogger) Fatal(v ...any)                 { fmt.Println(v...); os.Exit(1) }

// silence linters about unused imports when bools are referenced from
// other files only — Dialect/url/strings used via shared db.go.
var _ = url.Parse
var _ = strings.HasPrefix
