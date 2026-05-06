package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// BudgetIndex renders the budget tab for the requested month (default: current).
func (h *Handlers) BudgetIndex(c *gin.Context) {
	ctx := c.Request.Context()

	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	rows, err := h.store.MonthBudget(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, "month budget: %v", err)
		return
	}
	// Filter Income; group by GroupName preserving order.
	grouped := groupCategories(rows)

	incTotal, _ := h.store.TotalIncome(ctx, month)
	actual, _ := h.store.ActualIncomeForMonth(ctx, month)
	credit, _ := h.store.CreditCardActivityForMonth(ctx, month)

	// Filter credit rows with no activity.
	creditFiltered := credit[:0]
	for _, ca := range credit {
		if ca.PurchasesCents != 0 || ca.PaymentsCents != 0 {
			creditFiltered = append(creditFiltered, ca)
		}
	}

	var assigned int64
	for _, r := range rows {
		if !r.IsIncome {
			assigned += r.AssignedCents
		}
	}
	remain := incTotal - assigned
	gap := incTotal - actual

	prev := store.PrevMonth(month)
	t, _ := time.Parse("2006-01", month)
	next := t.AddDate(0, 1, 0).Format("2006-01")

	data := views.BudgetData{
		Month:       month,
		PrevMonth:   prev,
		NextMonth:   next,
		Estimated:   incTotal,
		Actual:      actual,
		Budgeted:    assigned,
		Remain:      remain,
		EstAct:      gap,
		CreditRows:  creditFiltered,
		GroupedRows: grouped,
	}
	render(c, http.StatusOK, views.BudgetPage(data))
}

// groupCategories takes the flat MonthBudget output and bins category
// rows by GroupName, preserving the order they came in (which already
// follows group sort_order).
func groupCategories(rows []store.CategoryBudget) [][]store.CategoryBudget {
	out := make([][]store.CategoryBudget, 0, 8)
	currentGroup := ""
	for _, r := range rows {
		if r.IsIncome {
			continue
		}
		if r.GroupName != currentGroup {
			out = append(out, []store.CategoryBudget{})
			currentGroup = r.GroupName
		}
		out[len(out)-1] = append(out[len(out)-1], r)
	}
	return out
}

// BudgetAssign updates the assigned amount for a category and returns
// the swapped row partial.
func (h *Handlers) BudgetAssign(c *gin.Context) {
	ctx := c.Request.Context()
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	cents, err := money.Parse(c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "invalid amount: %v", err)
		return
	}
	if err := h.store.SetAssigned(ctx, month, catID, cents); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// Re-fetch the row's current state (assigned, spent, available, etc.).
	rows, _ := h.store.MonthBudget(ctx, month)
	for _, r := range rows {
		if r.CategoryID == catID {
			render(c, http.StatusOK, views.BudgetRow(month, r))
			return
		}
	}
	c.String(http.StatusNotFound, "category not in month")
}
