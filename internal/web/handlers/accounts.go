package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/components/accountsoverview"
	"github.com/sbengtson/budget/internal/web/views"
)

// AccountsOverviewPartial renders just the account overview fragment for the
// sidebar. It is lazy-loaded on page load and re-fetched whenever a mutation
// emits the "accountsChanged" HTMX event. Month is empty so per-account links
// default to the current month on the transactions page.
func (h *Handlers) AccountsOverviewPartial(c *gin.Context) {
	accts, _ := h.store.ListAccounts(c.Request.Context(), true)
	render(c, http.StatusOK, accountsoverview.AccountsOverview(accts, ""))
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
		StartingBalance:   money.Format(a.StartingBalanceCents),
		IncludeInPaydown:  a.IncludeInPaydown,
		PaymentCategoryID: a.PaymentCategoryID,
		Categories:        cats,
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
	// Sidebar overview + accounts table refresh via the accountsChanged event.
	c.Header("HX-Trigger", "accountsChanged")
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
	// Close the modal (empty #modal) and let the sidebar overview + accounts
	// table refresh via the accountsChanged event, keeping the user on the
	// current page instead of redirecting to /accounts.
	c.Header("HX-Trigger", "accountsChanged")
	c.Writer.WriteHeader(http.StatusOK)
}
