package views

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/store"
)

// The budget grid sizes itself against #budget-table's width rather than the
// viewport. That distinction is invisible in the rendered markup unless you know
// to look for it, and getting it wrong fails quietly: the fixed tracks cannot
// shrink, so instead of reflowing they crush the category name to nothing and
// push a horizontal scrollbar onto the whole page. These guard the parts a
// future edit is most likely to undo by accident.

// renderRow renders one category row's markup for inspection.
func renderRow(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	r := store.CategoryBudget{CategoryID: 7, CategoryName: "Renter's Insurance"}
	if err := budgetRow("2026-08", r, false).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render row: %v", err)
	}
	return sb.String()
}

// The container declaration and the tier thresholds travel together: @container
// with no tier variants sizes nothing, and tier variants with no @container
// never match, so either half alone is silently inert.
func TestBudgetGridIsContainerQueried(t *testing.T) {
	var sb strings.Builder
	d := BudgetData{Month: "2026-08", Groups: []BudgetGroup{{ID: 1, Name: "Housing"}}}
	if err := BudgetTable(d).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render table: %v", err)
	}
	html := sb.String()

	if !regexp.MustCompile(`id="budget-table"[^>]*class="[^"]*@container`).MatchString(html) {
		t.Error("#budget-table must declare @container: the tier variants below resolve against it, " +
			"and without it none of them ever match")
	}
	for _, tier := range []string{"@min-[660px]:", "@min-[950px]:"} {
		if !strings.Contains(html, tier) {
			t.Errorf("table markup missing tier %q", tier)
		}
	}
}

// Every cell in the row must switch layout at the same widths the grid does. A
// cell left on a viewport breakpoint keeps its old placement while the tracks
// around it change, which is how the Assigned field came to be shown at 768px in
// a grid that had no column for it.
func TestBudgetRowCellsSwitchOnContainerNotViewport(t *testing.T) {
	html := renderRow(t)

	// Placement only. Purely visual md: variants are fine and deliberately left
	// alone — the drag handle and the hover-revealed row menu are pointer
	// affordances, which do track the device rather than the table's width.
	layout := regexp.MustCompile(`\bmd:(grid-cols|col-start|col-span|row-start|row-span|gap)[^\s"]*`)
	if got := layout.FindAllString(html, -1); len(got) > 0 {
		t.Errorf("row still switches layout on the viewport: %v\n"+
			"these must be @min-[660px]/@min-[950px] container variants, or the cell "+
			"and its grid track disagree about which layout is active", got)
	}
}

// Tier 1 has no Assigned column, so Available doubles as the assign control
// there and hands over the moment the Assigned field appears. If the two ever
// switch at different widths the row shows both controls or neither.
func TestAvailableHandsOffAtSameTierAsAssigned(t *testing.T) {
	html := renderRow(t)

	if !strings.Contains(html, "@min-[660px]:hidden") {
		t.Error("the tap-to-assign button must hide at the tier that introduces the Assigned field")
	}
	if !strings.Contains(html, "@min-[660px]:inline") {
		t.Error("the plain Available figure must appear at the tier that introduces the Assigned field")
	}
	if !strings.Contains(html, `class="js-assign col-start-3 row-start-1 hidden`) {
		t.Error("the Assigned field must be hidden by default and revealed by its tier variant")
	}
}

// The progress bar is the one cell that moves between tiers 2 and 3: stacked
// under the name until there is room for a column of its own.
func TestProgressStacksUnderNameUntilTier3(t *testing.T) {
	html := renderRow(t)

	if !strings.Contains(html, "col-start-1 row-start-2") {
		t.Error("progress must stack under the category name below tier 3")
	}
	if !strings.Contains(html, "@min-[950px]:col-start-4") {
		t.Error("progress must take its own column at tier 3")
	}
}
