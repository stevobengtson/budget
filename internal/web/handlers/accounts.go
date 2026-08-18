package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/components/accountsoverview"
	"github.com/sbengtson/budget/internal/web/views"
)

// AccountsOverviewPartial renders just the account overview fragment for the
// sidebar. It is lazy-loaded on page load and re-fetched whenever a mutation
// emits the "accountsChanged" HTMX event.
//
// Because the fragment is fetched separately from the page, it has no query
// string of its own: the whole view context (highlighted account, plus the
// month and category carried into the per-account links) comes from the
// HX-Current-URL header. HTMX sends that on the initial load and every
// accountsChanged refresh, so the highlight survives transaction edits.
// /budget and /transactions name the month param identically, so viewing a past
// budget month opens that same month's transactions.
func (h *Handlers) AccountsOverviewPartial(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	q := currentURLQuery(c)
	view := accountsoverview.ViewContext{
		Month:      q.Get("month"),
		AccountID:  parseIDQuery(q.Get("account")),
		CategoryID: parseIDQuery(q.Get("category")),

		OnTransactions: currentURLPath(c) == "/transactions",
		BankSync:       h.plaid.Enabled() && views.AddOnEnabled(ctx, "bank_sync"),
		ReviewCount:    views.ReviewCountFrom(ctx),
	}
	if view.BankSync {
		items, _ := h.store.ListPlaidItemsForUser(ctx, uid)
		for _, it := range items {
			if it.Status == store.PlaidItemLoginRequired || it.Status == store.PlaidItemPendingExpiration {
				view.ReconnectItems = append(view.ReconnectItems, it)
			}
		}
	}
	render(c, http.StatusOK, accountsoverview.AccountsOverview(accts, view))
}

func (h *Handlers) AccountsNew(c *gin.Context) {
	d := views.AccountFormData{Type: string(store.TypeChecking)}
	render(c, http.StatusOK, views.AccountForm(d))
}

func (h *Handlers) AccountsEdit(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	a, err := h.store.GetAccount(ctx, uid, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	d := views.AccountFormData{
		Editing: true, ID: a.ID, Name: a.Name, Type: string(a.Type),
		StartingBalance: money.Format(ctx, a.StartingBalanceCents),
	}
	if a.CreditLimitCents != nil {
		d.CreditLimit = money.Format(ctx, *a.CreditLimitCents)
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
	if err := h.store.ArchiveAccount(c.Request.Context(), currentUserID(c), id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// Sidebar overview + accounts table refresh via the accountsChanged event.
	c.Header("HX-Trigger", "accountsChanged")
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) upsertAccount(c *gin.Context, id int64) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	a := store.Account{ID: id, Name: c.PostForm("name"), Type: store.AccountType(c.PostForm("type"))}

	if v := c.PostForm("starting_balance"); v != "" {
		cents, err := money.Parse(ctx, v)
		if err != nil {
			c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.invalid_balance"))
			return
		}
		// Liability convenience: positive owed → store negative.
		if (a.Type == store.TypeCredit || a.Type == store.TypeLoan) && cents > 0 {
			cents = -cents
		}
		a.StartingBalanceCents = cents
	}
	if v := c.PostForm("credit_limit"); v != "" {
		if cents, err := money.Parse(ctx, v); err == nil {
			a.CreditLimitCents = &cents
		}
	}
	// The paydown add-on owns rate, payment, plan membership and payment
	// category, and edits them on its own page — this form never carries them.
	// UpdateAccount writes the whole row, so they have to be read back and
	// passed through or an ordinary rename would wipe the plan.
	if id != 0 {
		if cur, err := h.store.GetAccount(ctx, uid, id); err == nil {
			a.AprBps = cur.AprBps
			a.MonthlyPaymentCents = cur.MonthlyPaymentCents
			a.IncludeInPaydown = cur.IncludeInPaydown
			a.PaymentCategoryID = cur.PaymentCategoryID
		}
	}

	if id == 0 {
		_, err := h.store.CreateAccount(ctx, uid, a)
		if err != nil {
			writeStoreErr(c, err)
			return
		}
	} else {
		if err := h.store.UpdateAccount(ctx, uid, a); err != nil {
			writeStoreErr(c, err)
			return
		}
	}
	// Close the modal (empty #modal) and let the sidebar overview + accounts
	// table refresh via the accountsChanged event, keeping the user on the
	// current page instead of redirecting to /accounts.
	c.Header("HX-Trigger", "accountsChanged")
	c.Writer.WriteHeader(http.StatusOK)
}
