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

// Verifies the budget category list scrolls when the cursor leaves the
// visible window.
func TestBudgetScrollsCategoryList(t *testing.T) {
	zone.NewGlobal()
	conn, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	gid, _ := s.CreateGroup(ctx, "Bills", 0)
	for i := 0; i < 30; i++ {
		_, _ = s.CreateCategory(ctx, store.Category{
			GroupID: gid,
			Name:    "Cat" + string(rune('A'+i%26)) + string(rune('0'+i%10)),
		})
	}

	m := New(s)
	// Small terminal: forces scrolling.
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 25})
	m = mAny.(Model)

	rowCount := len(m.budget.rows)
	if rowCount < 20 {
		t.Fatalf("expected >= 20 rows, got %d", rowCount)
	}

	// Initially cursor at 0, scrollOffset at 0, ↓ N more should appear.
	out := m.View()
	if !strings.Contains(out, "more below") {
		t.Errorf("expected scroll indicator 'more below'; got:\n%s", out)
	}
	if strings.Contains(out, "more above") {
		t.Errorf("should not show 'more above' at top; got:\n%s", out)
	}

	// Press End to jump to last row — should now show 'more above', no 'more below'.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = mAny.(Model)
	if m.budget.cursor != rowCount-1 {
		t.Errorf("end did not move cursor to last; got %d want %d", m.budget.cursor, rowCount-1)
	}
	out = m.View()
	if !strings.Contains(out, "more above") {
		t.Errorf("expected 'more above' at end; got:\n%s", out)
	}
	if strings.Contains(out, "more below") {
		t.Errorf("should not show 'more below' at end; got:\n%s", out)
	}

	// Home returns to top.
	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = mAny.(Model)
	if m.budget.cursor != 0 || m.budget.scrollOffset != 0 {
		t.Errorf("home should reset cursor + scroll, got cursor=%d offset=%d", m.budget.cursor, m.budget.scrollOffset)
	}
}

// Verifies the budget body never exceeds terminal height (which would push
// the tab bar off screen) when many groups + categories combine.
func TestBudgetBodyFitsInHeight(t *testing.T) {
	zone.NewGlobal()
	conn, _ := db.Open(":memory:")
	defer func() { _ = conn.Close() }()
	s := store.New(conn)
	ctx := context.Background()

	// Create 8 groups × 5 categories = 40 categories, lots of group headers.
	for g := 0; g < 8; g++ {
		gid, _ := s.CreateGroup(ctx, "Grp"+string(rune('A'+g)), int64(g))
		for c := 0; c < 5; c++ {
			_, _ = s.CreateCategory(ctx, store.Category{
				GroupID: gid,
				Name:    "Cat" + string(rune('A'+g)) + string(rune('0'+c)),
			})
		}
	}

	height := 30
	m := New(s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: height})
	m = mAny.(Model)

	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) > height+1 { // allow trailing newline
		t.Errorf("View has %d lines, exceeds terminal height %d", len(lines), height)
	}

	// Tab bar should still be present at the very top.
	if !strings.Contains(strings.Join(lines[:3], "\n"), "Budget") {
		t.Errorf("tab bar missing or pushed off top; first 3 lines:\n%s",
			strings.Join(lines[:min(3, len(lines))], "\n"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
