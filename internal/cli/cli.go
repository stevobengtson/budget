// Package cli holds cobra command builders shared by the tui and web
// binaries: root flags, config resolution, and the db/migrate/seed admin
// commands. It imports only internal/core packages, never a UI package, so
// linking it adds no UI dependencies to either binary.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/config"
)

// App carries shared CLI state (config file path + viper instance) and builds
// cobra commands wired to it.
type App struct {
	cfgFile string
	v       *viper.Viper
}

// NewApp constructs an App with a fresh viper instance.
func NewApp() *App {
	return &App{v: viper.New()}
}

// Root builds the root command with persistent flags and config initialization.
// The caller adds subcommands and sets a default RunE (its UI launch).
func (a *App) Root(use, short, long string) *cobra.Command {
	root := &cobra.Command{
		Use:          use,
		Short:        short,
		Long:         long,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&a.cfgFile, "config", "",
		"config file (default: ./budget.yaml or $XDG_CONFIG_HOME/budget/config.yaml)")
	root.PersistentFlags().String("db", "",
		"database DSN (SQLite path or postgres://...). Overrides config and env.")
	root.PersistentFlags().String("log-level", "",
		"log level (debug|info|warn|error)")

	cobra.CheckErr(a.v.BindPFlag("db.dsn", root.PersistentFlags().Lookup("db")))
	cobra.CheckErr(a.v.BindPFlag("log.level", root.PersistentFlags().Lookup("log-level")))

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		a.initConfig()
		return nil
	}
	return root
}

func (a *App) initConfig() {
	if a.cfgFile != "" {
		a.v.SetConfigFile(a.cfgFile)
	} else {
		for _, p := range config.DefaultConfigSearchPaths() {
			a.v.AddConfigPath(p)
		}
		a.v.SetConfigName("budget")
		a.v.SetConfigType("yaml")
		_ = a.v.MergeInConfig()
		a.v.SetConfigName("config")
	}
	if err := a.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	}
}

// ResolvedConfig returns the fully-resolved configuration.
func (a *App) ResolvedConfig() (config.Config, error) {
	return config.Load(a.v)
}
