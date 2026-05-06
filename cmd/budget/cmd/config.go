package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect resolved configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the resolved config (flags > env > file > defaults)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := resolvedConfig()
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

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path of the loaded config file (or empty if none)",
	RunE: func(cmd *cobra.Command, args []string) error {
		used := v.ConfigFileUsed()
		if used == "" {
			fmt.Fprintln(os.Stderr, "(no config file loaded)")
			return nil
		}
		fmt.Println(used)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}
