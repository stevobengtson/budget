// Package format holds presentation helpers shared by the tui and web
// clients so wording (goal summaries, date labels) stays consistent.
package format

import (
	"time"

	"github.com/sbengtson/budget/internal/core/money"
)

// GoalDateLayout is the month/year layout used when displaying a goal due date.
const GoalDateLayout = "Jan 2006"

// Goal holds the formatted pieces of a category goal. Callers assemble them
// however their medium requires (plain text for the tui, HTML spans for web).
type Goal struct {
	Amount string // formatted goal amount, e.g. "$1,850.00"
	Due    string // formatted due date ("Jan 2006"), empty when none
	Need   string // formatted monthly need, e.g. "$150.00/mo", empty when target <= 0
}

// GoalFor builds the formatted goal pieces. ok is false when no goal is set.
func GoalFor(goalCents *int64, due *time.Time, monthlyTarget int64) (g Goal, ok bool) {
	if goalCents == nil {
		return Goal{}, false
	}
	g.Amount = money.Format(*goalCents)
	if due != nil {
		g.Due = due.Format(GoalDateLayout)
	}
	if monthlyTarget > 0 {
		g.Need = money.Format(monthlyTarget) + "/mo"
	}
	return g, true
}
