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

// BudgetIncomePanel renders /budget/income — the manage-income page.
func (h *Handlers) BudgetIncomePanel(c *gin.Context) {
	ctx := c.Request.Context()
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	rows, err := h.store.ListIncomes(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	total, _ := h.store.TotalIncome(ctx, month)
	actual, _ := h.store.ActualIncomeForMonth(ctx, month)

	// Sum assigned for budgeted figure (excludes Income category).
	budgetRows, _ := h.store.MonthBudget(ctx, month)
	var assigned int64
	for _, r := range budgetRows {
		if !r.IsIncome {
			assigned += r.AssignedCents
		}
	}

	prev := store.PrevMonth(month)
	t, _ := time.Parse("2006-01", month)
	next := t.AddDate(0, 1, 0).Format("2006-01")

	render(c, http.StatusOK, views.IncomePage(views.IncomeData{
		Month:     month,
		PrevMonth: prev,
		NextMonth: next,
		Today:     store.MonthKey(time.Now()),
		Rows:      rows,
		Total:     total,
		Actual:    actual,
		Budgeted:  assigned,
	}))
}

// BudgetIncomeNew returns the modal form for creating a new income line.
func (h *Handlers) BudgetIncomeNew(c *gin.Context) {
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}
	render(c, http.StatusOK, views.IncomeForm(views.IncomeFormData{Month: month}))
}

// BudgetIncomeEdit returns the modal form pre-filled with the row.
func (h *Handlers) BudgetIncomeEdit(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}
	rows, err := h.store.ListIncomes(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for _, r := range rows {
		if r.ID == id {
			render(c, http.StatusOK, views.IncomeForm(views.IncomeFormData{
				Editing: true, ID: r.ID, Month: month,
				Name:   r.Name,
				Amount: money.Format(r.AmountCents),
			}))
			return
		}
	}
	c.String(http.StatusNotFound, "income row not found in month %s", month)
}

func (h *Handlers) BudgetIncomeCreate(c *gin.Context) {
	ctx := c.Request.Context()
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}
	name := c.PostForm("name")
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	cents, err := money.Parse(c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "amount: %v", err)
		return
	}
	if _, err := h.store.CreateIncome(ctx, store.Income{
		Month: month, Name: name, AmountCents: cents,
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/budget/income?month="+month)
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) BudgetIncomeUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}
	name := c.PostForm("name")
	cents, err := money.Parse(c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "amount: %v", err)
		return
	}
	if err := h.store.UpdateIncome(ctx, store.Income{
		ID: id, Month: month, Name: name, AmountCents: cents,
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/budget/income?month="+month)
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) BudgetIncomeDelete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteIncome(c.Request.Context(), id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Writer.WriteHeader(http.StatusOK)
}
