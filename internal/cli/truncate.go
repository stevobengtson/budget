package cli

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// gooseVersionTable holds the applied-migration history. It is the one table
// truncate must leave alone: wiping it would make goose believe the schema is
// unmigrated and try to re-apply every migration over the existing tables.
const gooseVersionTable = "goose_db_version"

// truncateCmd builds the `db truncate` subcommand: empty every table but keep
// the schema and the migration history.
//
// This is not `db reset`. Reset rolls the schema all the way down and back up,
// which is slow and, more importantly, depends on every Down migration being
// correct. Truncate only touches rows, so it is fast and cannot leave the schema
// in a half-migrated state.
func (a *App) truncateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "truncate",
		Short: "Delete every row but keep the schema and migration history (DESTRUCTIVE)",
		Long: `Empty every table in the public schema, keeping the schema itself and the
goose_db_version history.

All tables are truncated in a single statement with CASCADE, which is what makes
the foreign keys between them a non-issue: Postgres does not check references
that are being emptied in the same command, so no delete ordering is needed.
RESTART IDENTITY also rewinds the sequences, so ids start from 1 again.

Rows inserted by a migration rather than by the app (the global Income category
from 00005) are re-created afterwards, since truncating removes them and no
migration will run again to put them back.`,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := a.ResolvedConfig()
			if err != nil {
				return err
			}
			conn, _, err := a.openForAdmin()
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			ctx := context.Background()
			tables, err := publicTables(ctx, conn)
			if err != nil {
				return err
			}
			if len(tables) == 0 {
				return fmt.Errorf("no tables to truncate — run `db migrate` first")
			}

			fmt.Printf("truncating %d tables in %s\n", len(tables), redactDSN(cfg.DB.DSN))
			if err := truncateAll(ctx, conn, tables); err != nil {
				return err
			}
			if err := seedGlobalIncome(ctx, conn); err != nil {
				return err
			}
			fmt.Println("truncated successfully")
			return nil
		},
	}
}

// publicTables lists the tables to empty: everything the app owns in the public
// schema, minus goose's bookkeeping.
func publicTables(ctx context.Context, conn *sql.DB) ([]string, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT tablename FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> $1
		 ORDER BY tablename`, gooseVersionTable)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, quoteIdent(name))
	}
	return out, rows.Err()
}

// truncateAll empties every named table in one statement. Doing them together
// (rather than one TRUNCATE per table) is what sidesteps the foreign keys —
// Postgres rejects truncating a referenced table on its own, but is happy when
// every referencing table is in the same command. CASCADE covers anything not
// listed, which after publicTables should be nothing.
func truncateAll(ctx context.Context, conn *sql.DB, tables []string) error {
	stmt := "TRUNCATE TABLE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	return nil
}

// seedGlobalIncome restores the pre-auth Income group and category that
// migration 00005 inserts. They carry a NULL user_id and are claimed by the
// first user to register (ClaimOrphanData), so the seed command and a fresh
// signup both depend on them existing. The inserts mirror 00005's, including
// its NOT EXISTS guards, so running this twice is harmless.
func seedGlobalIncome(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO category_groups(name, sort_order)
		 SELECT 'Income', -100
		 WHERE NOT EXISTS (SELECT 1 FROM category_groups WHERE name = 'Income')`); err != nil {
		return fmt.Errorf("restore income group: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO categories(group_id, name, is_income, sort_order)
		 SELECT g.id, 'Income', TRUE, 0
		 FROM category_groups g
		 WHERE g.name = 'Income'
		   AND NOT EXISTS (SELECT 1 FROM categories WHERE name = 'Income' AND is_income = TRUE)`); err != nil {
		return fmt.Errorf("restore income category: %w", err)
	}
	return nil
}

// quoteIdent wraps a table name in double quotes for interpolation into a
// TRUNCATE, which cannot take identifiers as bind parameters. The names come
// from pg_tables rather than user input, but doubling any embedded quote keeps
// that from being the only thing standing between here and a broken statement.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// redactDSN strips the password from a DSN so the command can echo which
// database it is about to empty without printing a credential.
func redactDSN(dsn string) string {
	at := strings.LastIndex(dsn, "@")
	scheme := strings.Index(dsn, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return dsn
	}
	creds := dsn[scheme+3 : at]
	if colon := strings.Index(creds, ":"); colon >= 0 {
		creds = creds[:colon] + ":***"
	}
	return dsn[:scheme+3] + creds + dsn[at:]
}
