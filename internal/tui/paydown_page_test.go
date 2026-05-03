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

func TestPaydownPagination(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	apr := int64(2099)
	pay := int64(80000)
	_, _ = s.CreateAccount(ctx, store.Account{
		Name: "Visa", Type: store.TypeCredit,
		StartingBalanceCents: -42_856_59,
		AprBps:               &apr,
		MonthlyPaymentCents:  &pay,
		IncludeInPaydown:     true,
	})

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mAny.(Model)

	now := time.Now()
	first := now.Format("Jan 2006")
	month13 := now.AddDate(0, 12, 0).Format("Jan 2006")

	out := m.View()
	if !strings.Contains(out, first) {
		t.Errorf("expected first-page month %s; got:\n%s", first, out)
	}

	// Page forward; should now show months 13+ onward.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = mAny.(Model)
	page2 := m.View()
	if !strings.Contains(page2, month13) {
		t.Errorf("page 2 should show %s; got:\n%s", month13, page2)
	}

	// Wrap back with PgUp.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mAny.(Model)
	if !strings.Contains(m.View(), first) {
		t.Error("PgUp should return to first page")
	}
}
