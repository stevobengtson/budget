package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/cli"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web"
)

// setupLogging installs a JSON slog handler on stderr (captured by journald)
// at the configured level. This finally gives the app real structured logs;
// the request-logging middleware writes through this logger.
func setupLogging(level string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

// setupSentry initializes error reporting when a DSN is configured. An empty
// DSN (dev default) is a no-op. Init failure is logged, not fatal — monitoring
// must never take the app down. Returns a flush func to run on shutdown.
func setupSentry(cfg config.Config) func() {
	if cfg.Sentry.DSN == "" {
		return func() {}
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.Sentry.DSN,
		Environment:      cfg.Sentry.Environment,
		EnableTracing:    cfg.Sentry.TracesSampleRate > 0,
		TracesSampleRate: cfg.Sentry.TracesSampleRate,
	})
	if err != nil {
		slog.Warn("sentry init failed; continuing without error reporting", "err", err)
		return func() {}
	}
	return func() { sentry.Flush(2 * time.Second) }
}

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
		setupLogging(cfg.Log.Level)
		defer setupSentry(cfg)()
		a := addr
		if a == "" {
			a = cfg.Web.Addr
		}
		conn, dialect, err := db.Open(cfg.DB.DSN, true)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer func() { _ = conn.Close() }()

		s := store.New(conn)

		// Log the schema version the server actually booted against. db.Open ran
		// migrations (shouldMigrate=true); printing the resolved dialect + version
		// makes a mismatched/unmigrated database obvious from the logs instead of
		// surfacing later as a missing-column query error.
		ver, verErr := db.MigrateVersion(conn, dialect)
		if verErr != nil {
			fmt.Printf("budget web — WARNING: could not read migration version: %v\n", verErr)
		} else {
			fmt.Printf("budget web — schema dialect=%s version=%d\n", dialect, ver)
		}

		srv := web.NewServer(s, cfg)

		// SIGINT/SIGTERM starts a graceful stop: the HTTP server drains, then the
		// Plaid polling worker's context is cancelled and waited on. The worker is
		// the app's first background goroutine — it must not die mid-sync (the
		// cursor-in-transaction design tolerates it, but a clean stop is free).
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		var wg sync.WaitGroup
		if plaidSvc := srv.Plaid(); plaidSvc.Enabled() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				plaidSvc.RunWorker(ctx, cfg.Plaid.PollInterval, srv.BankSyncEntitled())
			}()
		}

		httpSrv := &http.Server{Addr: a, Handler: srv.Handler()}
		errCh := make(chan error, 1)
		go func() { errCh <- httpSrv.ListenAndServe() }()
		fmt.Printf("budget web — listening on http://localhost%s (db=%s)\n", a, cfg.DB.DSN)

		select {
		case err := <-errCh:
			stop()
			wg.Wait()
			return err
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := httpSrv.Shutdown(shutdownCtx)
			wg.Wait()
			if errors.Is(err, context.DeadlineExceeded) {
				return errors.New("shutdown timed out")
			}
			return err
		}
	}

	webCmd := &cobra.Command{
		Use:   "web",
		Short: "Launch the HTTP server (HTMX + Templ)",
		RunE:  func(c *cobra.Command, args []string) error { return launch() },
	}
	webCmd.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")

	// Bare `budget` (web binary) launches the server; the admin subcommands
	// (db migrate/status/seed, config) operate on the resolved config DSN so they
	// target the same database the server would.
	root.RunE = func(c *cobra.Command, args []string) error { return launch() }
	root.Flags().StringVar(&addr, "addr", "", "listen address (default: from config web.addr or :8080)")
	root.AddCommand(webCmd, app.DBCmd(), app.ConfigCmd())

	cobra.CheckErr(root.Execute())
}
