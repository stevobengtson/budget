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

// Reproduces user issue: press `c` on Paydown, pick a category, expect linkage.
func TestPaydownLinkCategoryViaCKey(t *testing.T) {
	zone.NewGlobal()
	conn, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	gid, _ := s.CreateGroup(ctx, "Bills", 0)
	_, _ = s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Visa Payment"})

	apr := int64(2099)
	fallback := int64(40_000)
	visaID, _ := s.CreateAccount(ctx, store.Account{
		Name: "Visa", Type: store.TypeCredit,
		StartingBalanceCents: -42_856_59,
		AprBps:               &apr,
		MonthlyPaymentCents:  &fallback,
		IncludeInPaydown:     true,
		// no PaymentCategoryID
	})

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mAny.(Model)

	// Press c.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = mAny.(Model)
	if m.paydown.mode != pdCategoryPick {
		t.Fatalf("after c: mode = %v, want pdCategoryPick (%v)", m.paydown.mode, pdCategoryPick)
	}

	// Cursor should default to 0 (none). Move down once → "Bills · Visa Payment".
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mAny.(Model)

	// Press enter to pick.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mAny.(Model)

	// Verify DB persisted.
	acct, err := s.GetAccount(ctx, visaID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.PaymentCategoryID == nil {
		t.Fatalf("payment_category_id still NULL after picking category; account: %+v", acct)
	}

	// Verify the warning is gone in the rendered view.
	out := m.View()
	if strings.Contains(out, "no category linked") {
		t.Errorf("expected warning gone after linking; got:\n%s", out)
	}
}
