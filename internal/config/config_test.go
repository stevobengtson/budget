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
	// Should end with budget.db.
	if !strings.HasSuffix(cfg.DB.DSN, "budget.db") {
		t.Errorf("default DSN: %q", cfg.DB.DSN)
	}
}
