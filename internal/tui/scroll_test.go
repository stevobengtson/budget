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

// Categories list scrolls when cursor leaves the visible window and never
// pushes the tab bar off the top.
func TestCategoriesScrolls(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	for g := 0; g < 5; g++ {
		gid, _ := s.CreateGroup(ctx, "G"+string(rune('A'+g)), int64(g))
		for c := 0; c < 6; c++ {
			_, _ = s.CreateCategory(ctx, store.Category{
				GroupID: gid,
				Name:    "Cat" + string(rune('A'+g)) + string(rune('0'+c)),
			})
		}
	}

	height := 22
	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: height})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	m = mAny.(Model)

	out := m.View()
	if !strings.Contains(out, "more below") {
		t.Errorf("expected 'more below' indicator; got:\n%s", out)
	}

	// Total lines should not exceed terminal height.
	lines := strings.Split(out, "\n")
	if len(lines) > height+1 {
		t.Errorf("Categories view = %d lines, exceeds height %d", len(lines), height)
	}
	if !strings.Contains(strings.Join(lines[:3], "\n"), "Categories") &&
		!strings.Contains(strings.Join(lines[:3], "\n"), "Budget") {
		t.Errorf("tab bar not visible in first 3 lines:\n%s", strings.Join(lines[:3], "\n"))
	}

	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = mAny.(Model)
	if !strings.Contains(m.View(), "more above") {
		t.Errorf("expected 'more above' after End; got:\n%s", m.View())
	}
}

// Paydown sections respect terminal height when many accounts are included.
func TestPaydownSectionsScroll(t *testing.T) {
	zone.NewGlobal()
	conn, _, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	apr := int64(1500)
	pay := int64(50_000)
	for i := 0; i < 5; i++ {
		_, _ = s.CreateAccount(ctx, store.Account{
			Name: "Card" + string(rune('A'+i)), Type: store.TypeCredit,
			StartingBalanceCents: -100_000,
			AprBps:               &apr,
			MonthlyPaymentCents:  &pay,
			IncludeInPaydown:     true,
		})
	}

	height := 30
	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: height})
	m = mAny.(Model)
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mAny.(Model)

	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) > height+2 {
		t.Errorf("Paydown view = %d lines, exceeds height %d", len(lines), height)
	}

	// Tab bar visible at top.
	if !strings.Contains(strings.Join(lines[:3], "\n"), "Paydown") {
		t.Errorf("tab bar/Paydown title missing; first 3 lines:\n%s",
			strings.Join(lines[:3], "\n"))
	}
}
