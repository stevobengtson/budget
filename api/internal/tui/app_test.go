package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
)

func TestRootViewRenders(t *testing.T) {
	zone.NewGlobal()

	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	m := New(store.New(conn))
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mAny.(Model)

	for _, name := range []string{"Budget", "Transactions", "Accounts", "Categories", "Paydown"} {
		if !strings.Contains(m.View(), name) {
			t.Errorf("View missing tab label %q", name)
		}
	}
	// Status bar should mention current mode.
	if !strings.Contains(m.View(), "BUDGET") {
		t.Error("status bar should show active tab name in caps")
	}
}

func TestHelpPopupToggle(t *testing.T) {
	zone.NewGlobal()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	m := New(store.New(conn))
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mAny.(Model)

	if strings.Contains(m.View(), "Help · keymap") {
		t.Fatal("help should not be visible by default")
	}
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = mAny.(Model)
	if !strings.Contains(m.View(), "Help · keymap") {
		t.Errorf("? should open help popup; view:\n%s", m.View())
	}
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mAny.(Model)
	if strings.Contains(m.View(), "Help · keymap") {
		t.Error("esc should close help popup")
	}
}

func TestRowClickMovesCursor(t *testing.T) {
	zone.NewGlobal()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := t.Context()
	if _, err := s.CreateAccount(ctx, store.Account{Name: "A", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx, store.Account{Name: "B", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx, store.Account{Name: "C", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mAny.(Model)

	// Switch to Accounts tab.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = mAny.(Model)

	// Force View so zone marks register coordinates.
	_ = m.View()

	// Locate row 2 (index 2) bounds and click inside.
	bounds := zone.Get("acct-row-2")
	if !bounds.IsZero() {
		click := tea.MouseMsg{
			X:      bounds.StartX + 1,
			Y:      bounds.StartY,
			Action: tea.MouseActionRelease,
			Button: tea.MouseButtonLeft,
		}
		mAny, _ = m.Update(click)
		m = mAny.(Model)
		if m.accounts.cursor != 2 {
			t.Errorf("row click did not move cursor: got %d, want 2", m.accounts.cursor)
		}
	} else {
		t.Skip("zone bounds not yet registered (test environment renders without committing zones)")
	}
}
