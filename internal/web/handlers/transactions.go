package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

const txPageSize = 50

func (h *Handlers) TransactionsIndex(c *gin.Context) {
	ctx := c.Request.Context()

	var acctPtr *int64
	if a := c.Query("account"); a != "" {
		if v, err := strconv.ParseInt(a, 10, 64); err == nil {
			acctPtr = &v
		}
	}
	// Default to current month unless ?all=1 is set. This matches the
	// Budget tab's behavior where the user lands on the current month
	// and navigates with prev/next links.
	month := c.Query("month")
	if month == "" && c.Query("all") != "1" {
		month = store.MonthKey(time.Now())
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}

	rows, err := h.store.ListTransactions(ctx, store.TxFilter{
		AccountID: acctPtr, Month: month, Limit: 5000,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	total := len(rows)
	start := (page - 1) * txPageSize
	end := start + txPageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	rows = rows[start:end]

	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	groups, _ := h.store.ListGroups(ctx)

	prev, next := "", ""
	if month != "" {
		prev = store.PrevMonth(month)
		t, _ := time.Parse("2006-01", month)
		next = t.AddDate(0, 1, 0).Format("2006-01")
	}
	data := views.TransactionsData{
		Rows:          rows,
		Accounts:      accts,
		Categories:    cats,
		Groups:        groups,
		FilterAccount: acctPtr,
		FilterMonth:   month,
		PrevMonth:     prev,
		NextMonth:     next,
		Today:         store.MonthKey(time.Now()),
		Page:          page,
		PageSize:      txPageSize,
		Total:         total,
	}
	render(c, http.StatusOK, views.TransactionsPage(data))
}

func (h *Handlers) TransactionsNew(c *gin.Context) {
	accts, _ := h.store.ListAccounts(c.Request.Context(), false)
	cats, _ := h.store.ListCategories(c.Request.Context(), false)
	d := views.TxFormData{
		Date:       time.Now().Format("2006-01-02"),
		Accounts:   accts,
		Categories: cats,
	}
	if len(accts) > 0 {
		d.AccountID = accts[0].ID
	}
	// Default to the currently filtered account, when one is selected and still
	// present in the (non-archived) options.
	if filtered := txAccountFromRequest(c); filtered != nil {
		for _, a := range accts {
			if a.ID == *filtered {
				d.AccountID = *filtered
				break
			}
		}
	}
	render(c, http.StatusOK, views.TransactionForm(d))
}

func (h *Handlers) TransactionsCreate(c *gin.Context) {
	if err := h.upsertTransaction(c, 0); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Header("HX-Trigger", "accountsChanged")
	// Re-render full row list scoped by current filters.
	h.renderTxRows(c)
}

func (h *Handlers) TransactionsEdit(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	t, err := h.store.GetTransaction(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, "tx not found")
		return
	}
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	d := views.TxFormData{
		Editing:           true,
		ID:                t.ID,
		Date:              t.Date.Format("2006-01-02"),
		AccountID:         t.AccountID,
		CategoryID:        t.CategoryID,
		TransferAccountID: t.TransferAccountID,
		Accounts:          accts,
		Categories:        cats,
	}
	// A transfer's category lives on the outflow (spending) leg. When editing
	// the inflow leg, surface the pair's category so it's visible and editable.
	if t.TransferPairID != nil && t.CategoryID == nil {
		if pair, err := h.store.GetTransaction(ctx, *t.TransferPairID); err == nil {
			d.CategoryID = pair.CategoryID
		}
	}
	if t.Payee != nil {
		d.Payee = *t.Payee
	}
	if t.Notes != nil {
		d.Notes = *t.Notes
	}
	if t.OutflowCents > 0 {
		d.Outflow = money.Format(t.OutflowCents)
	}
	if t.InflowCents > 0 {
		d.Inflow = money.Format(t.InflowCents)
	}
	render(c, http.StatusOK, views.TransactionForm(d))
}

func (h *Handlers) TransactionsUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.upsertTransaction(c, id); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	// A successful update changed balances; refresh the sidebar overview on any
	// response path (including the reload-failure fallback below).
	c.Header("HX-Trigger", "accountsChanged")
	// Swap only the edited row (plus the paired transfer leg, if any) instead of
	// re-rendering the whole list. If the row can't be reloaded, fall back to a
	// full row refresh.
	t, err := h.store.GetTransaction(ctx, id)
	if err != nil {
		h.renderTxRows(c)
		return
	}
	var pair *store.Transaction
	if t.TransferPairID != nil {
		if p, err := h.store.GetTransaction(ctx, *t.TransferPairID); err == nil {
			pair = p
		}
	}
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	render(c, http.StatusOK, views.TxUpdateResult(txAccountFromRequest(c) != nil, *t, pair, accts, cats))
}

func (h *Handlers) TransactionsDelete(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteTransaction(ctx, id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// The delete button swaps the row out via its empty primary target; the
	// sidebar overview refreshes via the accountsChanged event.
	c.Header("HX-Trigger", "accountsChanged")
	c.Status(http.StatusOK)
}

func (h *Handlers) TransactionsToggleCleared(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	t, err := h.store.GetTransaction(ctx, id)
	if err != nil {
		c.String(http.StatusBadRequest, "not toggleable")
		return
	}
	// For a transfer leg SetCleared flips both legs; the paired row updates on
	// the next full render (HTMX partial-sync is handled separately).
	if err := h.store.SetCleared(ctx, id, !t.Cleared); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	t.Cleared = !t.Cleared
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	render(c, http.StatusOK, views.TransactionRow(*t, accts, cats, txAccountFromRequest(c) != nil, false))
}

// upsertTransaction creates (id==0) or updates a transaction or transfer
// from the form fields. Mirrors the TUI's parsing rules.
func (h *Handlers) upsertTransaction(c *gin.Context, id int64) error {
	ctx := c.Request.Context()
	dateStr := c.PostForm("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return err
	}
	acctID, _ := strconv.ParseInt(c.PostForm("account_id"), 10, 64)

	var catPtr *int64
	if v := c.PostForm("category_id"); v != "" {
		if cid, err := strconv.ParseInt(v, 10, 64); err == nil {
			catPtr = &cid
		}
	}
	var transferTo *int64
	if v := c.PostForm("transfer_to"); v != "" {
		if tid, err := strconv.ParseInt(v, 10, 64); err == nil {
			transferTo = &tid
		}
	}
	var outCents, inCents int64
	if v := c.PostForm("outflow"); v != "" {
		outCents, _ = money.Parse(v)
	}
	if v := c.PostForm("inflow"); v != "" {
		inCents, _ = money.Parse(v)
	}

	payee := c.PostForm("payee")
	notes := c.PostForm("notes")
	var payeePtr, notesPtr *string
	if payee != "" {
		payeePtr = &payee
	}
	if notes != "" {
		notesPtr = &notes
	}

	// Editing an existing transfer leg: update both legs in place rather than
	// deleting and recreating, so ids and the pair linkage survive.
	if id != 0 {
		existing, err := h.store.GetTransaction(ctx, id)
		if err != nil {
			return err
		}
		if existing.TransferPairID != nil {
			if transferTo == nil {
				return errInvalid("transfer requires a destination account")
			}
			if outCents+inCents <= 0 {
				return errInvalid("transfer amount required")
			}
			return h.store.UpdateTransfer(ctx, id, store.TransferLegEdit{
				Date: t, AccountID: acctID, TransferAccountID: *transferTo,
				OutflowCents: outCents, InflowCents: inCents,
				CategoryID: catPtr, Notes: notesPtr,
			})
		}
	}

	if transferTo != nil {
		amount := outCents + inCents
		if amount <= 0 {
			return errInvalid("transfer amount required")
		}
		fromID, toID := acctID, *transferTo
		if outCents == 0 {
			fromID, toID = *transferTo, acctID
		}
		_, _, err := h.store.CreateTransfer(ctx, store.TransferInput{
			Date: t, FromAccountID: fromID, ToAccountID: toID,
			AmountCents: amount, CategoryID: catPtr, Notes: notesPtr,
		})
		return err
	}

	tx := store.Transaction{
		ID: id, Date: t, AccountID: acctID, CategoryID: catPtr,
		Payee: payeePtr, Notes: notesPtr,
		OutflowCents: outCents, InflowCents: inCents,
	}
	if id == 0 {
		_, err := h.store.CreateTransaction(ctx, tx)
		return err
	}
	return h.store.UpdateTransaction(ctx, tx)
}

// renderTxRows writes the row HTML for <tbody id="tx-rows"> followed by
// an out-of-band swap that empties the #modal container — so the modal
// form disappears after a successful save.
// Filters are read from the HX-Current-URL header so the active account
// and month filters are preserved after create/update.
func (h *Handlers) renderTxRows(c *gin.Context) {
	ctx := c.Request.Context()

	var acctPtr *int64
	month := store.MonthKey(time.Now())
	if currentURL := c.GetHeader("HX-Current-URL"); currentURL != "" {
		if u, err := url.Parse(currentURL); err == nil {
			if a := u.Query().Get("account"); a != "" {
				if v, err := strconv.ParseInt(a, 10, 64); err == nil {
					acctPtr = &v
				}
			}
			if m := u.Query().Get("month"); m != "" {
				month = m
			} else if u.Query().Get("all") == "1" {
				month = ""
			}
		}
	}

	rows, _ := h.store.ListTransactions(ctx, store.TxFilter{
		AccountID: acctPtr, Month: month, Limit: txPageSize,
	})
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	c.Status(http.StatusOK)
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, t := range rows {
		_ = views.TransactionRow(t, accts, cats, acctPtr != nil, false).Render(ctx, c.Writer)
	}
	_, _ = c.Writer.WriteString(`<div id="modal" class="modal-mount" hx-swap-oob="true"></div>`)
}

// txAccountFromRequest returns the account filter reflected by the current
// transactions view (from the HX-Current-URL header), or nil when no account
// filter is active. Used to hide the redundant Account column and to default a
// new transaction's account to the filtered one.
func txAccountFromRequest(c *gin.Context) *int64 {
	if currentURL := c.GetHeader("HX-Current-URL"); currentURL != "" {
		if u, err := url.Parse(currentURL); err == nil {
			if a := u.Query().Get("account"); a != "" {
				if v, err := strconv.ParseInt(a, 10, 64); err == nil {
					return &v
				}
			}
		}
	}
	return nil
}

type errInvalid string

func (e errInvalid) Error() string { return string(e) }
