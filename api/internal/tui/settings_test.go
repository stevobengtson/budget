package tui

import (
	"context"
	"testing"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/settings"
	"github.com/sbengtson/budget/internal/core/store"
)

func newSettingsStore(t *testing.T) *store.Store {
	t.Helper()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return store.New(conn)
}

func TestSettingsModelSavesAndClears(t *testing.T) {
	s := newSettingsStore(t)
	ctx := context.Background()
	a1, _ := s.CreateAccount(ctx, store.Account{Name: "A", Type: store.TypeChecking})
	_, _ = s.CreateAccount(ctx, store.Account{Name: "B", Type: store.TypeChecking})
	gid, _ := s.CreateGroup(ctx, "G", 0)
	c1, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Food"})

	m := newSettingsModel(s)
	if err := m.Refresh(); err != nil {
		t.Fatal(err)
	}
	m.setAccountID(a1)
	m.setCategoryID(c1)
	if err := m.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if v, _, _ := s.GetSetting(ctx, settings.DefaultAccountKey); v == "" {
		t.Fatalf("account setting not persisted")
	}
	if v, _, _ := s.GetSetting(ctx, settings.DefaultCategoryKey); v == "" {
		t.Fatalf("category setting not persisted")
	}

	m.Reset()
	if err := m.Save(); err != nil {
		t.Fatalf("save reset: %v", err)
	}
	if _, ok, _ := s.GetSetting(ctx, settings.DefaultAccountKey); ok {
		t.Fatalf("account setting should be cleared")
	}
	if _, ok, _ := s.GetSetting(ctx, settings.DefaultCategoryKey); ok {
		t.Fatalf("category setting should be cleared")
	}
}
