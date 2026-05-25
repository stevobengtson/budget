package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"

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

	zone.NewGlobal()

	// The DB is opened inside the bubbletea event loop so that a
	// "Loading budget…" screen is rendered first. Close() releases the
	// connection (if one was successfully established) on exit.
	boot := tui.NewBootstrap(cfg.DB.DSN, 0)
	defer boot.Close()

	if _, err := tea.NewProgram(boot, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return err
	}
	return nil
}
