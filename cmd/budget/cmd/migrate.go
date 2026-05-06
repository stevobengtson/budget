package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/db"
)

var (
	migrateFrom string
	migrateTo   string
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Copy all data between two databases (SQLite ↔ Postgres)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if migrateFrom == "" || migrateTo == "" {
			return fmt.Errorf("both --from and --to are required")
		}
		ctx := context.Background()
		src, srcDialect, err := db.Open(migrateFrom)
		if err != nil {
			return fmt.Errorf("open source: %w", err)
		}
		defer func() { _ = src.Close() }()
		dst, dstDialect, err := db.Open(migrateTo)
		if err != nil {
			return fmt.Errorf("open dest: %w", err)
		}
		defer func() { _ = dst.Close() }()

		fmt.Fprintf(os.Stderr, "migrating %s → %s ...\n", srcDialect, dstDialect)
		if err := db.CopyAll(ctx, src, srcDialect, dst, dstDialect); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "done.")
		return nil
	},
}

func init() {
	migrateCmd.Flags().StringVar(&migrateFrom, "from", "", "source DSN (SQLite path or postgres URL)")
	migrateCmd.Flags().StringVar(&migrateTo, "to", "", "destination DSN")
	rootCmd.AddCommand(migrateCmd)
}
