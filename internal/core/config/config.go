// Package config loads runtime configuration from (in precedence order):
// CLI flag > environment variable > config file > defaults.
//
// The app targets Postgres only; db.dsn defaults to a local Postgres instance
// (postgres://postgres:postgres@127.0.0.1:5432/budget) so a developer with the
// standard local database gets a working setup with no config file required.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// DefaultPostgresDSN is the db.dsn used when nothing else is configured. It
// points at a local Postgres dev instance.
const DefaultPostgresDSN = "postgres://postgres:postgres@127.0.0.1:5432/budget?sslmode=disable"

// Config holds resolved settings used by both the TUI and the web server.
type Config struct {
	DB struct {
		DSN string `mapstructure:"dsn"`
	} `mapstructure:"db"`
	Web struct {
		Addr    string `mapstructure:"addr"`
		BaseURL string `mapstructure:"base_url"`
		Level   string `mapstructure:"level"`
	} `mapstructure:"web"`
	Mail struct {
		Driver       string `mapstructure:"driver"`
		From         string `mapstructure:"from"`
		ResendAPIKey string `mapstructure:"resend_api_key"`
	} `mapstructure:"mail"`
	Auth struct {
		SessionTTL   time.Duration `mapstructure:"session_ttl"`
		TokenTTL     time.Duration `mapstructure:"token_ttl"`
		CookieSecure bool          `mapstructure:"cookie_secure"`
	} `mapstructure:"auth"`
	Stripe struct {
		SecretKey      string `mapstructure:"secret_key"`
		PublishableKey string `mapstructure:"publishable_key"`
		WebhookSecret  string `mapstructure:"webhook_secret"`
		// PriceIDs holds the Stripe Price IDs the app subscribes customers to.
		// Base is the core $4.99/mo plan; add-on price IDs will join here later.
		PriceIDs struct {
			Base string `mapstructure:"base"`
		} `mapstructure:"price_ids"`
	} `mapstructure:"stripe"`
	Log struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"log"`
}

// Load reads from the supplied viper instance (which the caller has
// already populated with flag bindings + env prefix), applies defaults,
// and unmarshals into a typed Config.
func Load(v *viper.Viper) (Config, error) {
	v.SetDefault("db.dsn", DefaultPostgresDSN)
	v.SetDefault("web.addr", ":8080")
	v.SetDefault("web.base_url", "http://localhost:8080")
	v.SetDefault("web.level", gin.ReleaseMode)
	v.SetDefault("mail.driver", "console")
	v.SetDefault("mail.from", "Budget <noreply@example.com>")
	v.SetDefault("mail.resend_api_key", "")
	v.SetDefault("auth.session_ttl", "720h")
	v.SetDefault("auth.token_ttl", "1h")
	v.SetDefault("auth.cookie_secure", false)
	v.SetDefault("stripe.secret_key", "")
	v.SetDefault("stripe.publishable_key", "")
	v.SetDefault("stripe.webhook_secret", "")
	v.SetDefault("stripe.price_ids.base", "")
	v.SetDefault("log.level", "info")

	v.SetEnvPrefix("BUDGET")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
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
