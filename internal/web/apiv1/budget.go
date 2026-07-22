package apiv1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/store"
)

type budgetResponse struct {
	Month     string        `json:"month"`
	PrevMonth string        `json:"prevMonth"`
	NextMonth string        `json:"nextMonth"`
	Summary   budgetSummary `json:"summary"`
	Groups    []budgetGroup `json:"groups"`
}

// budgetSummary is the top-of-page banner: income for the month, how much of it
// is assigned, and what's left to assign. All integer cents.
type budgetSummary struct {
	IncomeCents    int64 `json:"incomeCents"`
	BudgetedCents  int64 `json:"budgetedCents"`
	RemainingCents int64 `json:"remainingCents"`
}

type budgetGroup struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	Categories []budgetCategory `json:"categories"`
}

type budgetCategory struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	AssignedCents  int64  `json:"assignedCents"`
	SpentCents     int64  `json:"spentCents"`
	AvailableCents int64  `json:"availableCents"`
	GoalCents      *int64 `json:"goalCents"`
	RolloverMode   string `json:"rolloverMode"`
}

// Budget returns the envelope-budget view for a month (default: current). It
// mirrors the web budget page: category groups with assigned/spent/available,
// plus a summary banner. The system Income group is excluded from Groups and
// surfaced as summary.incomeCents instead. Transactions live behind the account
// drill-in, so they're not part of this payload.
func (a *API) Budget(c *gin.Context) {
	ctx := c.Request.Context()
	uid := c.GetInt64(contextUserID)

	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	} else if _, err := time.Parse("2006-01", month); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "month must be formatted YYYY-MM.")
		return
	}

	rows, err := a.store.MonthBudget(ctx, uid, month)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load the budget.")
		return
	}
	groups, err := a.store.ListGroups(ctx, uid)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load the budget.")
		return
	}

	// Bin category rows by group. The system Income group is surfaced in the
	// summary, not the table, matching the web.
	incomeGroupID := int64(-1)
	byGroup := make(map[int64][]store.CategoryBudget)
	var budgeted int64
	for _, r := range rows {
		if r.IsIncome {
			incomeGroupID = r.GroupID
			continue
		}
		byGroup[r.GroupID] = append(byGroup[r.GroupID], r)
		budgeted += r.AssignedCents
	}

	// ListGroups drives order and includes empty groups (as the web does).
	outGroups := make([]budgetGroup, 0, len(groups))
	for _, g := range groups {
		if g.ID == incomeGroupID {
			continue
		}
		cats := make([]budgetCategory, 0, len(byGroup[g.ID]))
		for _, r := range byGroup[g.ID] {
			cats = append(cats, budgetCategory{
				ID:             r.CategoryID,
				Name:           r.CategoryName,
				AssignedCents:  r.AssignedCents,
				SpentCents:     r.SpentCents,
				AvailableCents: r.AvailableCents,
				GoalCents:      r.GoalCents,
				RolloverMode:   r.RolloverMode,
			})
		}
		outGroups = append(outGroups, budgetGroup{ID: g.ID, Name: g.Name, Categories: cats})
	}

	income, _ := a.store.TotalIncome(ctx, uid, month)
	parsed, _ := time.Parse("2006-01", month)

	c.JSON(http.StatusOK, budgetResponse{
		Month:     month,
		PrevMonth: store.PrevMonth(month),
		NextMonth: parsed.AddDate(0, 1, 0).Format("2006-01"),
		Summary: budgetSummary{
			IncomeCents:    income,
			BudgetedCents:  budgeted,
			RemainingCents: income - budgeted,
		},
		Groups: outGroups,
	})
}
