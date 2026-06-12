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

func TestTransactionsGroupedByDay(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()
	chk, _ := s.CreateAccount(ctx, store.Account{Name: "Chk", Type: store.TypeChecking, StartingBalanceCents: 10_000_000})
	gid, _ := s.CreateGroup(ctx, "M", 0)
	cat, _ := s.CreateCategory(ctx, store.Category{GroupID: gid, Name: "Misc"})

	// Two transactions on May 25, one on May 26 — same month so they share a
	// page under the default month filter.
	d25 := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)
	d26 := time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)
	for _, d := range []time.Time{d25, d25, d26} {
		_, _ = s.CreateTransaction(ctx, store.Transaction{
			Date: d, AccountID: chk, CategoryID: &cat, OutflowCents: 1000,
		})
	}

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = mAny.(Model)
	// Switch to the transactions tab and align the month filter to May 2026.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mAny.(Model)
	m.transactions.filterMonth = "2026-05"
	_ = m.transactions.Refresh()

	out := m.transactions.viewList()

	h25 := "Mon, May 25, 2026"
	h26 := "Tue, May 26, 2026"
	if strings.Count(out, h25) != 1 {
		t.Errorf("expected exactly one %q header, got %d\n%s", h25, strings.Count(out, h25), out)
	}
	if strings.Count(out, h26) != 1 {
		t.Errorf("expected exactly one %q header, got %d\n%s", h26, strings.Count(out, h26), out)
	}
	// Column header row is still present; per-row Date column is gone.
	if !strings.Contains(out, "Category / Transfer") {
		t.Errorf("missing column header row\n%s", out)
	}
}

func TestTransferRowShowsDashInClearedColumn(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()
	from, _ := s.CreateAccount(ctx, store.Account{Name: "Chk", Type: store.TypeChecking, StartingBalanceCents: 10_000_000})
	to, _ := s.CreateAccount(ctx, store.Account{Name: "Visa", Type: store.TypeCredit})
	if _, _, err := s.CreateTransfer(ctx, store.TransferInput{
		Date:          time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		FromAccountID: from, ToAccountID: to, AmountCents: 5000,
	}); err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}

	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = mAny.(Model)
	m.transactions.filterMonth = "2026-05"
	_ = m.transactions.Refresh()

	if len(m.transactions.rows) == 0 {
		t.Fatal("expected transfer legs in rows")
	}
	out := m.transactions.viewList()
	if !strings.Contains(out, "-") {
		t.Errorf("expected a dash in the cleared column for transfers\n%s", out)
	}
	if strings.Contains(out, "✓") {
		t.Errorf("transfers must not show a cleared checkbox\n%s", out)
	}
}

func TestComputePageStartsRespectsLineBudget(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	var rows []store.Transaction
	// 40 transactions spread two-per-day across 20 days.
	for i := 0; i < 40; i++ {
		rows = append(rows, store.Transaction{Date: base.AddDate(0, 0, i/2)})
	}

	budget := 10
	starts := computePageStarts(rows, budget)
	if len(starts) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(starts))
	}
	if starts[0] != 0 {
		t.Fatalf("first page must start at 0, got %d", starts[0])
	}

	// Each page's rendered height (transactions + per-day headers) must fit.
	for p := 0; p < len(starts); p++ {
		start := starts[p]
		end := len(rows)
		if p+1 < len(starts) {
			end = starts[p+1]
		}
		if start >= end {
			t.Fatalf("page %d empty: [%d,%d)", p, start, end)
		}
		lines, lastDay := 0, ""
		for i := start; i < end; i++ {
			day := rows[i].Date.Format("2006-01-02")
			lines++
			if day != lastDay {
				lines++
				lastDay = day
			}
		}
		if lines > budget {
			t.Errorf("page %d uses %d lines, budget %d", p, lines, budget)
		}
	}
}
