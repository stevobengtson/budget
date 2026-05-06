package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/web/views"
)

func (h *Handlers) AccountsIndex(c *gin.Context) {
	ctx := c.Request.Context()
	rows, _ := h.store.ListAccounts(ctx, false)
	cats, _ := h.store.ListCategories(ctx, false)
	var assets, liab, avail int64
	for _, a := range rows {
		if a.Type.IsLiability() {
			liab += a.BalanceCents
		} else {
			assets += a.BalanceCents
		}
		if a.CreditLimitCents != nil {
			v := a.BalanceCents + *a.CreditLimitCents
			if v > 0 {
				avail += v
			}
		}
	}
	d := views.AccountsData{
		Rows: rows, Categories: cats,
		Assets: assets, Liabilities: liab,
		Difference: assets + liab,
		AvailCredit: avail,
	}
	render(c, http.StatusOK, views.AccountsPage(d))
}

func (h *Handlers) AccountsNew(c *gin.Context) {
	cats, _ := h.store.ListCategories(c.Request.Context(), false)
	d := views.AccountFormData{
		Type: string(store.TypeChecking), Categories: cats,
	}
	render(c, http.StatusOK, views.AccountForm(d))
}

func (h *Handlers) AccountsEdit(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	a, err := h.store.GetAccount(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	cats, _ := h.store.ListCategories(ctx, false)
	d := views.AccountFormData{
		Editing: true, ID: a.ID, Name: a.Name, Type: string(a.Type),
		StartingBalance: money.Format(a.StartingBalanceCents),
		IncludeInPaydown: a.IncludeInPaydown,
		PaymentCategoryID: a.PaymentCategoryID,
		Categories: cats,
	}
	if a.CreditLimitCents != nil {
		d.CreditLimit = money.Format(*a.CreditLimitCents)
	}
	if a.AprBps != nil {
		d.APR = strconv.FormatFloat(float64(*a.AprBps)/100.0, 'f', 2, 64)
	}
	if a.MonthlyPaymentCents != nil {
		d.MonthlyPayment = money.Format(*a.MonthlyPaymentCents)
	}
	render(c, http.StatusOK, views.AccountForm(d))
}

func (h *Handlers) AccountsCreate(c *gin.Context) { h.upsertAccount(c, 0) }
func (h *Handlers) AccountsUpdate(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	h.upsertAccount(c, id)
}

func (h *Handlers) AccountsArchive(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.ArchiveAccount(c.Request.Context(), id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) upsertAccount(c *gin.Context, id int64) {
	ctx := c.Request.Context()
	a := store.Account{ID: id, Name: c.PostForm("name"), Type: store.AccountType(c.PostForm("type"))}

	if v := c.PostForm("starting_balance"); v != "" {
		cents, err := money.Parse(v)
		if err != nil {
			c.String(http.StatusBadRequest, "starting balance: %v", err)
			return
		}
		// Liability convenience: positive owed → store negative.
		if (a.Type == store.TypeCredit || a.Type == store.TypeLoan) && cents > 0 {
			cents = -cents
		}
		a.StartingBalanceCents = cents
	}
	if v := c.PostForm("credit_limit"); v != "" {
		if cents, err := money.Parse(v); err == nil {
			a.CreditLimitCents = &cents
		}
	}
	if v := c.PostForm("apr"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			bps := int64(f * 100)
			a.AprBps = &bps
		}
	}
	if v := c.PostForm("monthly_payment"); v != "" {
		if cents, err := money.Parse(v); err == nil {
			a.MonthlyPaymentCents = &cents
		}
	}
	a.IncludeInPaydown = c.PostForm("include_in_paydown") == "1"
	if v := c.PostForm("payment_category_id"); v != "" {
		if cid, err := strconv.ParseInt(v, 10, 64); err == nil {
			a.PaymentCategoryID = &cid
		}
	}

	if id == 0 {
		_, err := h.store.CreateAccount(ctx, a)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := h.store.UpdateAccount(ctx, a); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	c.Header("HX-Redirect", "/accounts")
	c.Writer.WriteHeader(http.StatusOK)
}
