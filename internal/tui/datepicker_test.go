package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
)

func TestTransactionDatePickerFillsField(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()
	_, _ = s.CreateAccount(ctx, store.Account{Name: "Chk", Type: store.TypeChecking})

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = mAny.(Model)
	// Switch to Transactions tab.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mAny.(Model)
	// New transaction.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = mAny.(Model)
	if m.transactions.mode != txForm {
		t.Fatalf("expected txForm, got %v", m.transactions.mode)
	}
	// Cursor should be on field 0 (Date). Press space to open picker.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m = mAny.(Model)
	if m.transactions.mode != txDatePick {
		t.Fatalf("expected txDatePick after space, got %v", m.transactions.mode)
	}

	// Force an explicit date so the test is deterministic.
	m.transactions.dp.SetTime(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))

	// Press enter to commit.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mAny.(Model)
	if m.transactions.mode != txForm {
		t.Fatalf("expected txForm after enter, got %v", m.transactions.mode)
	}
	got := m.transactions.form.fields[0].input.Value()
	if got != "2026-06-15" {
		t.Errorf("Date field = %q, want 2026-06-15", got)
	}
}
