package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/core/db"
)

// MigrateCmd builds the `migrate` command (copy all data between two DBs).
func (a *App) MigrateCmd() *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Copy all data between two databases (SQLite ↔ Postgres)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if from == "" || to == "" {
				return fmt.Errorf("both --from and --to are required")
			}
			ctx := context.Background()
			src, srcDialect, err := db.Open(from)
			if err != nil {
				return fmt.Errorf("open source: %w", err)
			}
			defer func() { _ = src.Close() }()
			dst, dstDialect, err := db.Open(to)
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
	cmd.Flags().StringVar(&from, "from", "", "source DSN (SQLite path or postgres URL)")
	cmd.Flags().StringVar(&to, "to", "", "destination DSN")
	return cmd
}
