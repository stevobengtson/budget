package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
)

func TestPaydownUsesBudgetForPayments(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, store.Account{Name: "Chk", Type: store.TypeChecking, StartingBalanceCents: 1_000_000})

	gid, _ := s.CreateGroup(ctx, "Bills", 0)
	visaPay, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Visa Payment"})

	apr := int64(2099)
	fallback := int64(40_000)
	visa, _ := s.CreateAccount(ctx, store.Account{
		Name: "Visa", Type: store.TypeCredit,
		StartingBalanceCents: -42_856_59,
		AprBps:               &apr,
		MonthlyPaymentCents:  &fallback,
		IncludeInPaydown:     true,
		PaymentCategoryID:    &visaPay,
	})
	_ = visa

	// "Current" month is whatever time.Now() reports — set both spent + assigned for it
	// so projection picks up the override.
	now := time.Now()
	curMonth := store.MonthKey(now)
	_ = s.SetAssigned(ctx, curMonth, visaPay, 80_000)
	_, _ = s.CreateTransaction(ctx, store.Transaction{
		Date: now, AccountID: chk, CategoryID: &visaPay, OutflowCents: 80_000,
	})

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mAny.(Model)

	out := m.View()
	if !strings.Contains(out, "$800.00") {
		t.Errorf("expected $800.00 (from budget) in payments; got:\n%s", out)
	}
	if !strings.Contains(out, "spent") {
		t.Errorf("expected source marker 'spent' in current-month row; got:\n%s", out)
	}
	if !strings.Contains(out, "category Visa Payment") {
		t.Errorf("expected account header to mention linked category; got:\n%s", out)
	}
}
