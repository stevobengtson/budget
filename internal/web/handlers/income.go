package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// BudgetIncomeNew returns the transient add-income row.
func (h *Handlers) BudgetIncomeNew(c *gin.Context) {
	render(c, http.StatusOK, views.IncomeNewRow(monthOrNow(c)))
}

// BudgetIncomeCreate inserts an income entry and returns its row + OOB banner.
func (h *Handlers) BudgetIncomeCreate(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	cents, err := money.Parse(c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "amount: %v", err)
		return
	}
	max, err := h.store.MaxIncomeSortOrder(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	id, err := h.store.CreateIncome(ctx, store.Income{
		Month: month, Name: name, AmountCents: cents, SortOrder: max + 1,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	row := store.Income{ID: id, Month: month, Name: name, AmountCents: cents, SortOrder: max + 1}
	render(c, http.StatusOK, views.BudgetIncomeRowResult(month, row, data))
}

// BudgetIncomeUpdate field-merges name and/or amount (whichever the inline
// edit submitted) and returns the refreshed row + OOB banner.
func (h *Handlers) BudgetIncomeUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	rows, err := h.store.ListIncomes(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var cur *store.Income
	for i := range rows {
		if rows[i].ID == id {
			cur = &rows[i]
			break
		}
	}
	if cur == nil {
		c.String(http.StatusNotFound, "income row not found in month %s", month)
		return
	}
	if v, ok := c.GetPostForm("name"); ok {
		name := strings.TrimSpace(v)
		if name == "" {
			c.String(http.StatusBadRequest, "name required")
			return
		}
		cur.Name = name
	}
	if v, ok := c.GetPostForm("amount"); ok {
		cents, err := money.Parse(v)
		if err != nil {
			c.String(http.StatusBadRequest, "amount: %v", err)
			return
		}
		cur.AmountCents = cents
	}
	if err := h.store.UpdateIncome(ctx, *cur); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetIncomeRowResult(month, *cur, data))
}

// BudgetIncomeDelete removes an income entry; the row is dropped and the banner
// refreshes out of band.
func (h *Handlers) BudgetIncomeDelete(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteIncome(ctx, id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetIncomeDeleteResult(data))
}

// BudgetIncomeCopyPrev copies the previous month's entries into this month and
// re-renders the region. The (month, name) pair is unique, so entries already
// present are updated to the previous month's amount/sort_order instead of
// erroring.
func (h *Handlers) BudgetIncomeCopyPrev(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	prev := store.PrevMonth(month)

	prevRows, err := h.store.ListIncomes(ctx, prev)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	curRows, err := h.store.ListIncomes(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	existing := make(map[string]store.Income, len(curRows))
	for _, r := range curRows {
		existing[r.Name] = r
	}
	for _, r := range prevRows {
		if cur, ok := existing[r.Name]; ok {
			cur.AmountCents = r.AmountCents
			cur.SortOrder = r.SortOrder
			if err := h.store.UpdateIncome(ctx, cur); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			continue
		}
		if _, err := h.store.CreateIncome(ctx, store.Income{
			Month: month, Name: r.Name, AmountCents: r.AmountCents, SortOrder: r.SortOrder,
		}); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetRegion(data))
}
