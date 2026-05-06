package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the terminal UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func init() { rootCmd.AddCommand(tuiCmd) }

func runTUI() error {
	cfg, err := resolvedConfig()
	if err != nil {
		return err
	}
	conn, dialect, err := db.Open(cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = conn.Close() }()

	zone.NewGlobal()

	sd := store.DialectSQLite
	if dialect == db.DialectPostgres {
		sd = store.DialectPostgres
	}
	s := store.NewWithDialect(conn, sd)
	m := tui.New(s)
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return err
	}
	return nil
}
