package settings_test

import (
	"context"
	"testing"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/settings"
	"github.com/sbengtson/budget/internal/core/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return store.New(conn)
}

func TestResolveDefaultAccount_NoAccounts(t *testing.T) {
	s := newStore(t)
	if _, err := settings.ResolveDefaultAccount(context.Background(), s); err != settings.ErrNoAccounts {
		t.Fatalf("err = %v, want ErrNoAccounts", err)
	}
}

func TestResolveDefaultAccount_FallbackToLowestID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	// Names chosen so alphabetical order != id order.
	bID, _ := s.CreateAccount(ctx, store.Account{Name: "B-bank", Type: store.TypeChecking})
	_, _ = s.CreateAccount(ctx, store.Account{Name: "A-bank", Type: store.TypeChecking})

	got, err := settings.ResolveDefaultAccount(ctx, s)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != bID {
		t.Fatalf("got %d, want %d (lowest id, not alphabetical)", got, bID)
	}
}

func TestResolveDefaultAccount_UsesStoredID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, _ = s.CreateAccount(ctx, store.Account{Name: "A", Type: store.TypeChecking})
	id2, _ := s.CreateAccount(ctx, store.Account{Name: "B", Type: store.TypeChecking})

	if err := s.SetSetting(ctx, settings.DefaultAccountKey, "2"); err != nil {
		t.Fatal(err)
	}
	got, err := settings.ResolveDefaultAccount(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if got != id2 {
		t.Fatalf("got %d, want %d", got, id2)
	}
}

func TestResolveDefaultAccount_StaleIDFallsBack(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	id1, _ := s.CreateAccount(ctx, store.Account{Name: "Keeper", Type: store.TypeChecking})
	id2, _ := s.CreateAccount(ctx, store.Account{Name: "Doomed", Type: store.TypeChecking})

	_ = s.SetSetting(ctx, settings.DefaultAccountKey, "999999")
	got, _ := settings.ResolveDefaultAccount(ctx, s)
	if got != id1 {
		t.Fatalf("bad id: got %d, want %d (lowest)", got, id1)
	}

	// Archived id stored => also fall back to lowest non-archived.
	_ = s.ArchiveAccount(ctx, id2)
	_ = s.SetSetting(ctx, settings.DefaultAccountKey, "2")
	got, _ = settings.ResolveDefaultAccount(ctx, s)
	if got != id1 {
		t.Fatalf("archived id should fall back: got %d, want %d", got, id1)
	}
}

func TestResolveDefaultCategory_NoneReturnsNil(t *testing.T) {
	s := newStore(t)
	got, err := settings.ResolveDefaultCategory(context.Background(), s)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestResolveDefaultCategory_FallbackToLowestID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	gid, _ := s.CreateGroup(ctx, "G", 0)
	c1, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "B-cat"})
	_, _ = s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "A-cat"})

	got, err := settings.ResolveDefaultCategory(ctx, s)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got == nil || *got != c1 {
		t.Fatalf("got %v, want &%d", got, c1)
	}
}

func TestResolveDefaultCategory_StaleIDFallsBack(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	gid, _ := s.CreateGroup(ctx, "G", 0)
	cKeep, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Keep"})

	_ = s.SetSetting(ctx, settings.DefaultCategoryKey, "999999")
	got, _ := settings.ResolveDefaultCategory(ctx, s)
	if got == nil || *got != cKeep {
		t.Fatalf("got %v, want &%d (fallback)", got, cKeep)
	}
}
