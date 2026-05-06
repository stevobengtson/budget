package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/web"
)

var webAddr string

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Launch the HTTP server (HTMX + Templ)",
	RunE: func(c *cobra.Command, args []string) error {
		cfg, err := resolvedConfig()
		if err != nil {
			return err
		}
		addr := webAddr
		if addr == "" {
			addr = cfg.Web.Addr
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
		fmt.Printf("budget web — listening on http://localhost%s (db=%s)\n", addr, cfg.DB.DSN)
		return http.ListenAndServe(addr, srv.Handler())
	},
}

func init() {
	webCmd.Flags().StringVar(&webAddr, "addr", "", "listen address (default: from config web.addr or :8080)")
	rootCmd.AddCommand(webCmd)
}
