package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/cli"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web"
)

func main() {
	app := cli.NewApp()
	root := app.Root("budget",
		"Personal budget — web server",
		`budget (web) serves the HTMX + Templ web UI.

Configuration is read from budget.yaml, BUDGET_* env vars, and CLI flags.
The db/migrate/seed/config admin commands are available as subcommands.`)

	var addr string
	launch := func() error {
		cfg, err := app.ResolvedConfig()
		if err != nil {
			return err
		}
		a := addr
		if a == "" {
			a = cfg.Web.Addr
		}
		conn, dialect, err := db.Open(cfg.DB.DSN)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer func() { _ = conn.Close() }()

		sd := store.DialectSQLite
		if dialect == db.DialectPostgres {
			sd = store.DialectPostgres
		}
		s := store.NewWithDialect(conn, sd)

		srv := web.NewServer(s)
		fmt.Printf("budget web — listening on http://localhost%s (db=%s)\n", a, cfg.DB.DSN)
		return http.ListenAndServe(a, srv.Handler())
	}

	webCmd := &cobra.Command{
		Use:   "web",
		Short: "Launch the HTTP server (HTMX + Templ)",
		RunE:  func(c *cobra.Command, args []string) error { return launch() },
	}
	webCmd.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")

	// Bare `budget` (web binary) launches the server.
	root.RunE = func(c *cobra.Command, args []string) error { return launch() }
	root.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")

	root.AddCommand(webCmd, app.ConfigCmd(), app.DBCmd(), app.MigrateCmd())
	cobra.CheckErr(root.Execute())
}
