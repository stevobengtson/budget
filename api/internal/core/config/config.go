// Package config loads runtime configuration from (in precedence order):
// CLI flag > environment variable > config file > defaults.
//
// Defaults to a SQLite database at $XDG_CONFIG_HOME/budget/budget.db so
// first-time users get the same experience as before, with no config
// file required.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds resolved settings used by both the TUI and the web server.
type Config struct {
	DB struct {
		DSN string `mapstructure:"dsn"`
	} `mapstructure:"db"`
	Web struct {
		Addr string `mapstructure:"addr"`
	} `mapstructure:"web"`
	Log struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"log"`
	// Auth configures verification of BetterAuth-issued JWTs. The Go API never
	// issues tokens — it only verifies signatures against the JWKS and checks
	// the issuer/audience claims.
	Auth struct {
		JWKSURL  string `mapstructure:"jwks_url"`
		Issuer   string `mapstructure:"issuer"`
		Audience string `mapstructure:"audience"`
	} `mapstructure:"auth"`
}

// Load reads from the supplied viper instance (which the caller has
// already populated with flag bindings + env prefix), applies defaults,
// and unmarshals into a typed Config.
func Load(v *viper.Viper) (Config, error) {
	defaultDSN, _ := DefaultDBPath()
	v.SetDefault("db.dsn", defaultDSN)
	v.SetDefault("web.addr", ":8080")
	v.SetDefault("log.level", "info")
	v.SetDefault("auth.jwks_url", "http://localhost:3005/api/auth/jwks")
	v.SetDefault("auth.issuer", "http://localhost:3005")
	v.SetDefault("auth.audience", "budget-go-api")

	v.SetEnvPrefix("BUDGET")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

// DefaultDBPath returns the default SQLite location.
func DefaultDBPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "budget", "budget.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "budget", "budget.db"), nil
}

// DefaultConfigSearchPaths returns the directories Cobra/Viper should
// search for a config file (in order). Caller passes these to viper via
// AddConfigPath; the file name (without extension) is "config" or
// "budget".
func DefaultConfigSearchPaths() []string {
	paths := []string{"."}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		paths = append(paths, filepath.Join(x, "budget"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "budget"))
	}
	return paths
}
