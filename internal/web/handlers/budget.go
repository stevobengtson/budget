package handlers

import (
	"context"
	"fmt"
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

	data, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, "month budget: %v", err)
		return
	}
	_ = rows
	render(c, http.StatusOK, views.BudgetPage(data))
}

// budgetData loads everything needed to render the budget page for the
// requested month and returns the rendered view-model plus the flat row
// slice (callers that need to find a single row by ID can reuse it without
// a second store round-trip).
func (h *Handlers) budgetData(ctx context.Context, month string) (views.BudgetData, []store.CategoryBudget, error) {
	rows, err := h.store.MonthBudget(ctx, month)
	if err != nil {
		return views.BudgetData{}, nil, fmt.Errorf("month budget: %w", err)
	}
	grouped := groupCategories(rows)

	incTotal, _ := h.store.TotalIncome(ctx, month)
	actual, _ := h.store.ActualIncomeForMonth(ctx, month)
	credit, _ := h.store.CreditCardActivityForMonth(ctx, month)

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

	prev := store.PrevMonth(month)
	t, _ := time.Parse("2006-01", month)
	next := t.AddDate(0, 1, 0).Format("2006-01")

	return views.BudgetData{
		Month:       month,
		PrevMonth:   prev,
		NextMonth:   next,
		Estimated:   incTotal,
		Actual:      actual,
		Budgeted:    assigned,
		Remain:      incTotal - assigned,
		EstAct:      incTotal - actual,
		CreditRows:  creditFiltered,
		GroupedRows: grouped,
	}, rows, nil
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
// the swapped row partial plus out-of-band updates for the banner stats
// that move when an assignment changes (Budgeted, Remain).
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

	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetRegion(data))
}

// BudgetAssignCopyPrev replaces the current month's assignment for a
// single category with whatever was assigned in the previous month
// (defaults to 0 if there was no entry there). Returns the same region
// fragment used by BudgetAssign so the banner + totals stay in sync.
func (h *Handlers) BudgetAssignCopyPrev(c *gin.Context) {
	ctx := c.Request.Context()
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	prev := store.PrevMonth(month)
	prevCents, err := h.store.GetAssigned(ctx, prev, catID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SetAssigned(ctx, month, catID, prevCents); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetRegion(data))
}
