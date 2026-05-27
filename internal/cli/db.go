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
