package tui

import (
	"context"
	"fmt"
	"testing"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/settings"
	"github.com/sbengtson/budget/internal/core/store"
)

func TestTxStartFormUsesConfiguredDefaults(t *testing.T) {
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	_, _ = s.CreateAccount(ctx, store.Account{Name: "Aardvark", Type: store.TypeChecking})
	pick, _ := s.CreateAccount(ctx, store.Account{Name: "Zebra", Type: store.TypeChecking})
	gid, _ := s.CreateGroup(ctx, "G", 0)
	_, _ = s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Apples"})
	pickCat, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Zucchini"})

	if err := s.SetSetting(ctx, settings.DefaultAccountKey, fmt.Sprintf("%d", pick)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, settings.DefaultCategoryKey, fmt.Sprintf("%d", pickCat)); err != nil {
		t.Fatal(err)
	}

	m := newTxModel(s)
	// Refresh() loads data synchronously and returns nil; calling it directly
	// populates m.accounts and m.cats before startForm is invoked.
	_ = m.Refresh()

	m.startForm(nil)

	if got := m.form.fields[1].input.Value(); got != "Zebra" {
		t.Errorf("account default = %q, want \"Zebra\"", got)
	}
	if got := m.form.fields[2].input.Value(); got != "Zucchini" {
		t.Errorf("category default = %q, want \"Zucchini\"", got)
	}
}
