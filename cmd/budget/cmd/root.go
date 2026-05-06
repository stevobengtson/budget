package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/config"
)

var (
	cfgFile string
	v       = viper.New()
)

var rootCmd = &cobra.Command{
	Use:   "budget",
	Short: "Personal budget app — TUI and web frontends, SQLite or Postgres",
	Long: `budget is a small, keyboard-driven personal finance app.

By default, running "budget" with no subcommand launches the TUI. Use
"budget web" to start the HTTP server. Configuration is read from a
budget.yaml file (./budget.yaml or $XDG_CONFIG_HOME/budget/config.yaml),
overridden by BUDGET_* env vars and CLI flags.`,
	SilenceUsage: true,
	// Default to TUI when no subcommand given.
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

// Execute is invoked by cmd/budget/main.go.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"config file (default: ./budget.yaml or $XDG_CONFIG_HOME/budget/config.yaml)")
	rootCmd.PersistentFlags().String("db", "",
		"database DSN (SQLite path or postgres://...). Overrides config and env.")
	rootCmd.PersistentFlags().String("log-level", "",
		"log level (debug|info|warn|error)")

	cobra.CheckErr(v.BindPFlag("db.dsn", rootCmd.PersistentFlags().Lookup("db")))
	cobra.CheckErr(v.BindPFlag("log.level", rootCmd.PersistentFlags().Lookup("log-level")))
}

func initConfig() {
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		for _, p := range config.DefaultConfigSearchPaths() {
			v.AddConfigPath(p)
		}
		v.SetConfigName("budget")
		v.SetConfigType("yaml")
		// Allow $XDG/.config/budget/config.yaml as alternate name.
		_ = v.MergeInConfig()
		v.SetConfigName("config")
	}
	if err := v.ReadInConfig(); err != nil {
		// Missing file is fine; surface other errors.
		var notFound viper.ConfigFileNotFoundError
		if !errorsIs(err, &notFound) {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	}
}

// errorsIs is a tiny helper that avoids dragging in errors.As semantics
// for the one place we care about.
func errorsIs(err error, target any) bool {
	switch target.(type) {
	case *viper.ConfigFileNotFoundError:
		_, ok := err.(viper.ConfigFileNotFoundError)
		return ok
	}
	return false
}

func resolvedConfig() (config.Config, error) {
	return config.Load(v)
}
