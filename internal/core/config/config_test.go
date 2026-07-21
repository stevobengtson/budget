package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestPrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/non/existent/path/that/should/not/exist")
	t.Setenv("BUDGET_DB_DSN", "from-env.db")

	v := viper.New()
	cfg, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	// Env beats default.
	if cfg.DB.DSN != "from-env.db" {
		t.Errorf("env override: got %q, want from-env.db", cfg.DB.DSN)
	}
	if cfg.Web.Addr != ":8080" {
		t.Errorf("default web.addr: got %q, want :8080", cfg.Web.Addr)
	}
}

func TestFileBeatsDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "budget.yaml")
	if err := os.WriteFile(cfgPath, []byte("db:\n  dsn: from-file.db\nweb:\n  addr: \":7000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(cfgPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.DSN != "from-file.db" {
		t.Errorf("file: got %q, want from-file.db", cfg.DB.DSN)
	}
	if cfg.Web.Addr != ":7000" {
		t.Errorf("file: got %q, want :7000", cfg.Web.Addr)
	}
}

func TestDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	v := viper.New()
	cfg, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.DSN == "" {
		t.Error("expected default DSN")
	}
	// The app is Postgres-only; the default DSN points at a local Postgres.
	if cfg.DB.DSN != DefaultPostgresDSN {
		t.Errorf("default DSN: got %q, want %q", cfg.DB.DSN, DefaultPostgresDSN)
	}
	if !strings.HasPrefix(cfg.DB.DSN, "postgres://") {
		t.Errorf("default DSN should be a postgres URL: %q", cfg.DB.DSN)
	}
}

func TestAuthMailDefaults(t *testing.T) {
	v := viper.New()
	cfg, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web.BaseURL != "http://localhost:8080" {
		t.Errorf("base_url default: %q", cfg.Web.BaseURL)
	}
	if cfg.Mail.Driver != "console" {
		t.Errorf("mail driver default: %q", cfg.Mail.Driver)
	}
	if cfg.Auth.SessionTTL.Hours() != 720 {
		t.Errorf("session ttl default: %v", cfg.Auth.SessionTTL)
	}
	if cfg.Auth.TokenTTL.Hours() != 1 {
		t.Errorf("token ttl default: %v", cfg.Auth.TokenTTL)
	}
}

func TestResendKeyFromEnv(t *testing.T) {
	t.Setenv("BUDGET_MAIL_RESEND_API_KEY", "re_xyz")
	v := viper.New()
	cfg, _ := Load(v)
	if cfg.Mail.ResendAPIKey != "re_xyz" {
		t.Errorf("resend key from env: %q, %q", cfg.Mail.ResendAPIKey, cfg.Mail.Driver)
	}
}
