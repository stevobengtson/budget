package cmd

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/db"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Schema migrations (apply / rollback / status)",
	Long: `Manage the database schema using the embedded goose migrations.

These commands operate on the database identified by the resolved
config (--db flag, BUDGET_DB_DSN env var, or budget.yaml). To target a
different database temporarily, pass --db on the command line.`,
}

var dbUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	RunE: func(c *cobra.Command, args []string) error {
		conn, dialect, err := openForAdmin()
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		if err := db.MigrateUp(conn, dialect); err != nil {
			return err
		}
		v, _ := db.MigrateVersion(conn, dialect)
		fmt.Printf("ok — version %d\n", v)
		return nil
	},
}

var dbUpOneCmd = &cobra.Command{
	Use:   "up-one",
	Short: "Apply the next pending migration",
	RunE: func(c *cobra.Command, args []string) error {
		conn, dialect, err := openForAdmin()
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		if err := db.MigrateUpByOne(conn, dialect); err != nil {
			return err
		}
		v, _ := db.MigrateVersion(conn, dialect)
		fmt.Printf("ok — version %d\n", v)
		return nil
	},
}

var dbDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Roll back the most recently applied migration",
	RunE: func(c *cobra.Command, args []string) error {
		conn, dialect, err := openForAdmin()
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		if err := db.MigrateDown(conn, dialect); err != nil {
			return err
		}
		v, _ := db.MigrateVersion(conn, dialect)
		fmt.Printf("ok — version %d\n", v)
		return nil
	},
}

var dbResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Roll back to zero and re-apply all migrations (DESTRUCTIVE: wipes data)",
	RunE: func(c *cobra.Command, args []string) error {
		conn, dialect, err := openForAdmin()
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		if err := db.MigrateReset(conn, dialect); err != nil {
			return err
		}
		v, _ := db.MigrateVersion(conn, dialect)
		fmt.Printf("ok — version %d\n", v)
		return nil
	},
}

var dbStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print one line per migration (applied / pending)",
	RunE: func(c *cobra.Command, args []string) error {
		conn, dialect, err := openForAdmin()
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		return db.MigrateStatus(conn, dialect)
	},
}

var dbVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the current migration version",
	RunE: func(c *cobra.Command, args []string) error {
		conn, dialect, err := openForAdmin()
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
	},
}

func openForAdmin() (*sql.DB, db.Dialect, error) {
	cfg, err := resolvedConfig()
	if err != nil {
		return nil, 0, err
	}
	conn, dialect, err := db.OpenNoMigrate(cfg.DB.DSN)
	if err != nil {
		return nil, 0, fmt.Errorf("open db: %w", err)
	}
	return conn, dialect, nil
}

// anyDB is a tiny alias to keep the import surface narrow.

func init() {
	dbCmd.AddCommand(dbUpCmd, dbUpOneCmd, dbDownCmd, dbResetCmd, dbStatusCmd, dbVersionCmd)
	rootCmd.AddCommand(dbCmd)
}
