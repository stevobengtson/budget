package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/api"
	"github.com/sbengtson/budget/internal/api/middleware"
	"github.com/sbengtson/budget/internal/cli"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
)

func main() {
	app := cli.NewApp()
	root := app.Root("budget-api",
		"Personal budget — JSON API",
		`budget-api serves the multi-user JSON API.

Authentication/authorization is delegated to BetterAuth: incoming requests must
carry a Bearer JWT, which is verified against BetterAuth's JWKS (auth.jwks_url).
Configuration is read from budget.yaml, BUDGET_* env vars, and CLI flags.`)

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

		verifier, err := middleware.NewVerifier(
			context.Background(),
			cfg.Auth.JWKSURL,
			cfg.Auth.Issuer,
			cfg.Auth.Audience,
		)
		if err != nil {
			return fmt.Errorf("init auth verifier: %w", err)
		}

		srv := api.NewServer(s, verifier)
		fmt.Printf("budget api — listening on http://localhost%s (db=%s, jwks=%s)\n",
			a, cfg.DB.DSN, cfg.Auth.JWKSURL)
		return http.ListenAndServe(a, srv.Handler())
	}

	root.RunE = func(c *cobra.Command, args []string) error { return launch() }
	root.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")

	root.AddCommand(app.ConfigCmd(), app.DBCmd(), app.MigrateCmd())
	cobra.CheckErr(root.Execute())
}
