package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// ConfigCmd builds the `config` command group (show / path).
func (a *App) ConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect resolved configuration",
	}

	configShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Print the resolved config (flags > env > file > defaults)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.ResolvedConfig()
			if err != nil {
				return err
			}
			out, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}

	configPathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the path of the loaded config file (or empty if none)",
		RunE: func(cmd *cobra.Command, args []string) error {
			used := a.v.ConfigFileUsed()
			if used == "" {
				fmt.Fprintln(os.Stderr, "(no config file loaded)")
				return nil
			}
			fmt.Println(used)
			return nil
		},
	}

	configCmd.AddCommand(configShowCmd, configPathCmd)
	return configCmd
}
