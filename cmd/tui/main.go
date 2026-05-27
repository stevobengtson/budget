package main

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/spf13/cobra"

	"github.com/sbengtson/budget/internal/cli"
	"github.com/sbengtson/budget/internal/tui"
)

func main() {
	app := cli.NewApp()
	root := app.Root("budget",
		"Personal budget — terminal UI",
		`budget (tui) launches the keyboard-driven terminal UI.

Configuration is read from budget.yaml, BUDGET_* env vars, and CLI flags.
The db/migrate/seed/config admin commands are available as subcommands.`)

	launch := func() error {
		cfg, err := app.ResolvedConfig()
		if err != nil {
			return err
		}
		zone.NewGlobal()
		boot := tui.NewBootstrap(cfg.DB.DSN, 0)
		defer boot.Close()
		if _, err := tea.NewProgram(boot, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
			return err
		}
		return nil
	}

	tuiCmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the terminal UI",
		RunE:  func(c *cobra.Command, args []string) error { return launch() },
	}

	// Bare `budget` (tui binary) launches the TUI.
	root.RunE = func(c *cobra.Command, args []string) error { return launch() }

	root.AddCommand(tuiCmd, app.ConfigCmd(), app.DBCmd(), app.MigrateCmd())
	cobra.CheckErr(root.Execute())
}
