package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
)

func TestTransactionsPagination(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()
	chk, _ := s.CreateAccount(ctx, store.Account{Name: "Chk", Type: store.TypeChecking, StartingBalanceCents: 10_000_000})
	gid, _ := s.CreateGroup(ctx, "M", 0)
	cat, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Misc"})

	now := time.Now()
	for i := 0; i < 50; i++ {
		_, _ = s.CreateTransaction(ctx, store.Transaction{
			Date: now, AccountID: chk, CategoryID: &cat,
			Payee:        ptrStr(fmtRow(i)),
			OutflowCents: int64(100 + i),
		})
	}

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mAny.(Model)

	if m.transactions.pager.PerPage <= 0 {
		t.Fatalf("PerPage should be positive, got %d", m.transactions.pager.PerPage)
	}
	if m.transactions.pager.TotalPages < 2 {
		t.Fatalf("TotalPages should be ≥ 2 for 50 rows on small terminal, got %d", m.transactions.pager.TotalPages)
	}

	// Rows render newest-first. ROW49 was created last, so it appears at the
	// top of page 0.
	first := m.View()
	if !strings.Contains(first, "ROW49") {
		t.Errorf("page 0 missing newest row ROW49")
	}
	if strings.Contains(first, "ROW00") {
		t.Errorf("page 0 should not contain oldest row ROW00")
	}

	// PgDn should advance and reposition cursor.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = mAny.(Model)
	if m.transactions.pager.Page != 1 {
		t.Errorf("expected page 1 after pgdn, got %d", m.transactions.pager.Page)
	}

	// Cursor should now point at first row of page 1 (perPage index).
	if m.transactions.cursor != m.transactions.pager.PerPage {
		t.Errorf("cursor = %d, want %d (start of page 1)", m.transactions.cursor, m.transactions.pager.PerPage)
	}
}

func ptrStr(s string) *string { return &s }

func fmtRow(i int) string {
	return "ROW" + intStr(i)
}

func intStr(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	first := i / 10
	second := i % 10
	return string(rune('0'+first)) + string(rune('0'+second))
}
