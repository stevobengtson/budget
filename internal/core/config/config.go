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
		// TrustedProxies are the hops allowed to set X-Forwarded-For. In
		// production nginx terminates TLS on the same box and proxies to
		// loopback, so the default is loopback only.
		//
		// This is security-relevant, not cosmetic: Gin trusts every proxy by
		// default, which makes the client IP attacker-controlled and would let
		// anyone bypass per-IP rate limiting by varying a header.
		TrustedProxies []string `mapstructure:"trusted_proxies"`
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
		// ReauthWindow is how long proving a factor keeps the security screen's
		// sensitive actions unlocked.
		ReauthWindow time.Duration `mapstructure:"reauth_window"`
		// Lockout tunes the password-failure escalation. Threshold failures are
		// free; each one after that doubles the lock, starting at Base and
		// capped at Max. Window is how long a failure stays "recent".
		Lockout struct {
			Threshold int           `mapstructure:"threshold"`
			Base      time.Duration `mapstructure:"base"`
			Max       time.Duration `mapstructure:"max"`
			Window    time.Duration `mapstructure:"window"`
		} `mapstructure:"lockout"`
	} `mapstructure:"auth"`
	Stripe struct {
		SecretKey      string `mapstructure:"secret_key"`
		PublishableKey string `mapstructure:"publishable_key"`
		WebhookSecret  string `mapstructure:"webhook_secret"`
		// PriceIDs holds the Stripe Price IDs the app subscribes customers to.
		// Base is the core $4.99/mo plan; BankSync is the Plaid bank-sync add-on
		// billed as a second item on the base subscription.
		PriceIDs struct {
			Base     string `mapstructure:"base"`
			BankSync string `mapstructure:"bank_sync"`
		} `mapstructure:"price_ids"`
	} `mapstructure:"stripe"`
	Plaid struct {
		ClientID  string `mapstructure:"client_id"`
		SecretKey string `mapstructure:"secret_key"`
		// Environment selects the Plaid API host: "sandbox" or "production".
		Environment string `mapstructure:"environment"`
		// EncryptionKey encrypts Plaid access tokens at rest (AES-256-GCM).
		// 64 hex characters (32 bytes). Unset leaves the bank-sync feature inert.
		EncryptionKey string `mapstructure:"encryption_key"`
		// PollInterval is the fallback sync cadence when webhooks don't arrive.
		PollInterval time.Duration `mapstructure:"poll_interval"`
		// DaysRequested bounds the initial transaction history pull (1-730).
		DaysRequested int `mapstructure:"days_requested"`
	} `mapstructure:"plaid"`
	Log struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"log"`
	Sentry struct {
		// DSN is the Sentry project DSN. Empty disables error reporting
		// entirely (dev default) — no events are sent and no middleware wires in.
		DSN string `mapstructure:"dsn"`
		// Environment tags events (e.g. "production", "development").
		Environment string `mapstructure:"environment"`
		// TracesSampleRate in [0,1] enables performance tracing when > 0. Left
		// at 0 (off) to conserve the free-tier span quota; error reporting works
		// regardless.
		TracesSampleRate float64 `mapstructure:"traces_sample_rate"`
	} `mapstructure:"sentry"`
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
	v.SetDefault("auth.reauth_window", "15m")
	v.SetDefault("auth.lockout.threshold", 8)
	v.SetDefault("auth.lockout.base", "15m")
	v.SetDefault("auth.lockout.max", "24h")
	v.SetDefault("auth.lockout.window", "1h")
	// Loopback only: nginx terminates TLS on the same host and proxies to
	// 127.0.0.1:8080. Widen this only for a proxy you actually control —
	// anything listed here is trusted to report the client's IP honestly.
	v.SetDefault("web.trusted_proxies", []string{"127.0.0.1", "::1"})
	v.SetDefault("stripe.secret_key", "")
	v.SetDefault("stripe.publishable_key", "")
	v.SetDefault("stripe.webhook_secret", "")
	v.SetDefault("stripe.price_ids.base", "")
	v.SetDefault("stripe.price_ids.bank_sync", "")
	v.SetDefault("plaid.client_id", "")
	v.SetDefault("plaid.secret_key", "")
	v.SetDefault("plaid.environment", "sandbox")
	v.SetDefault("plaid.encryption_key", "")
	v.SetDefault("plaid.poll_interval", "6h")
	v.SetDefault("plaid.days_requested", 90)
	v.SetDefault("log.level", "info")
	v.SetDefault("sentry.dsn", "")
	v.SetDefault("sentry.environment", "development")
	v.SetDefault("sentry.traces_sample_rate", 0.0)

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
