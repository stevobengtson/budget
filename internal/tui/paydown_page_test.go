package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
)

func TestPaydownPagination(t *testing.T) {
	zone.NewGlobal()
	conn, _ := db.Open(":memory:")
	defer conn.Close()
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

	out := m.View()
	if !strings.Contains(out, "Apr 2026") {
		t.Errorf("expected first-page month Apr 2026; got:\n%s", out)
	}

	// Page forward; should now show months 13+ (Apr 2027 onward).
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = mAny.(Model)
	page2 := m.View()
	if strings.Contains(page2, "May 2026") {
		t.Error("page 2 should not show May 2026")
	}
	if !strings.Contains(page2, "Apr 2027") {
		t.Errorf("page 2 should show Apr 2027; got:\n%s", page2)
	}

	// Wrap back with PgUp.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mAny.(Model)
	if !strings.Contains(m.View(), "Apr 2026") {
		t.Error("PgUp should return to first page")
	}
}
