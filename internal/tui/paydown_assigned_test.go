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

// Verifies that assigned values in future months flow into the projection.
func TestPaydownPicksUpAssignedForFutureMonths(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	gid, _ := s.CreateGroup(ctx, "Bills", 0)
	visaPay, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Visa Payment"})

	apr := int64(2099)
	fallback := int64(40_000)
	_, _ = s.CreateAccount(ctx, store.Account{
		Name: "Visa", Type: store.TypeCredit,
		StartingBalanceCents: -42_856_59,
		AprBps:               &apr,
		MonthlyPaymentCents:  &fallback,
		IncludeInPaydown:     true,
		PaymentCategoryID:    &visaPay,
	})

	// Assign $900 to Visa Payment two months out.
	now := time.Now()
	twoOut := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 2, 0)
	_ = s.SetAssigned(ctx, store.MonthKey(twoOut), visaPay, 90_000)

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mAny.(Model)

	out := m.View()
	if !strings.Contains(out, "$900.00") {
		t.Errorf("expected $900.00 (assigned future month) in payments column; got:\n%s", out)
	}
	if !strings.Contains(out, "assigned") {
		t.Errorf("expected source marker 'assigned'; got:\n%s", out)
	}
}

// Verifies that overdraft on a checking account flows through paydown when
// the account is included and has APR set.
func TestPaydownTreatsOverdraftCheckingAsDebt(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	apr := int64(800) // 8% overdraft
	limit := int64(100_000)
	pay := int64(10_000)
	_, _ = s.CreateAccount(ctx, store.Account{
		Name: "Chk", Type: store.TypeChecking,
		StartingBalanceCents: -50_000, // overdrawn $500
		CreditLimitCents:     &limit,
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
	if !strings.Contains(out, "$500.00") {
		t.Errorf("expected overdraft start of $500.00 in projection; got:\n%s", out)
	}
	if !strings.Contains(out, "8.00% APR") {
		t.Errorf("expected 8%% APR in header; got:\n%s", out)
	}
}

// Verifies the paydown screen warns when an account is included but has no
// payment category linked (so all rows use fallback).
func TestPaydownWarnsWhenNoCategoryLinked(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	apr := int64(2099)
	fallback := int64(80_000)
	_, _ = s.CreateAccount(ctx, store.Account{
		Name: "Visa", Type: store.TypeCredit,
		StartingBalanceCents: -42_856_59,
		AprBps:               &apr,
		MonthlyPaymentCents:  &fallback,
		IncludeInPaydown:     true,
		// PaymentCategoryID intentionally nil
	})

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 60})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mAny.(Model)

	out := m.View()
	if !strings.Contains(out, "no category") {
		t.Errorf("expected 'no category' warning in header; got:\n%s", out)
	}
}
