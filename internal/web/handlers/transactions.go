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

	collapsed := sidebarCollapsed(c)

	var acctPtr *int64
	if a := c.Query("account"); a != "" {
		if v, err := strconv.ParseInt(a, 10, 64); err == nil {
			acctPtr = &v
		}
	}
	var catPtr *int64
	if v := c.Query("category"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			catPtr = &id
		}
	}
	// Both filters are optional: the filter bar lets the user clear back to an
	// unscoped month view, so an unfiltered request lists every account's
	// transactions rather than redirecting away.
	//
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

	uid := currentUserID(c)
	rows, err := h.store.ListTransactions(ctx, uid, store.TxFilter{
		AccountID: acctPtr, CategoryID: catPtr, Month: month, Limit: 5000,
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

	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	groups, _ := h.store.ListGroups(ctx, uid)

	prev, next := "", ""
	if month != "" {
		prev = store.PrevMonth(month)
		t, _ := time.Parse("2006-01", month)
		next = t.AddDate(0, 1, 0).Format("2006-01")
	}
	data := views.TransactionsData{
		Rows:           rows,
		Accounts:       accts,
		Categories:     cats,
		Groups:         groups,
		FilterAccount:  acctPtr,
		FilterCategory: catPtr,
		FilterMonth:    month,
		PrevMonth:      prev,
		NextMonth:      next,
		Today:          store.MonthKey(time.Now()),
		Page:           page,
		PageSize:       txPageSize,
		Total:          total,
	}
	render(c, http.StatusOK, views.TransactionsPage(data, collapsed))
}

func (h *Handlers) TransactionsNew(c *gin.Context) {
	uid := currentUserID(c)
	accts, _ := h.store.ListAccounts(c.Request.Context(), uid, false)
	cats, _ := h.store.ListCategories(c.Request.Context(), uid, false)
	d := views.TxFormData{
		TxType:     "expense",
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
	d.CategoryID = defaultCategoryForNewTx(c, cats)
	render(c, http.StatusOK, views.TransactionForm(d))
}

// defaultCategoryForNewTx preselects the category a new transaction starts
// with: the one the transactions view is filtered to, since adding from a
// category view means the user is already working within that category. cats
// holds the selectable (non-archived) categories, so a filter pointing at an
// archived category falls through to no selection.
func defaultCategoryForNewTx(c *gin.Context, cats []store.Category) *int64 {
	filtered := txFilterFromRequest(c, "category")
	if filtered == nil {
		return nil
	}
	for _, cat := range cats {
		if cat.ID == *filtered {
			return filtered
		}
	}
	return nil
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
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	t, err := h.store.GetTransaction(ctx, uid, id)
	if err != nil {
		c.String(http.StatusNotFound, "tx not found")
		return
	}
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	d := views.TxFormData{
		Editing:    true,
		ID:         t.ID,
		Date:       t.Date.Format("2006-01-02"),
		AccountID:  t.AccountID,
		CategoryID: t.CategoryID,
		Accounts:   accts,
		Categories: cats,
	}
	// Map the stored legs onto the single (type, amount) the form uses. Transfers
	// are always shown from the source's perspective — Account = "from",
	// Transfer to = "to", amount = the outflow — so editing the inflow
	// (destination) leg is normalized to keep the amount an outflow.
	switch {
	case t.TransferAccountID != nil:
		d.TxType = "transfer"
		if t.OutflowCents > 0 {
			d.TransferAccountID = t.TransferAccountID
			d.Amount = money.Format(t.OutflowCents)
		} else {
			src := *t.TransferAccountID
			dst := t.AccountID
			d.AccountID = src
			d.TransferAccountID = &dst
			d.Amount = money.Format(t.InflowCents)
		}
	case t.InflowCents > 0:
		d.TxType = "income"
		d.Amount = money.Format(t.InflowCents)
	default:
		d.TxType = "expense"
		d.Amount = money.Format(t.OutflowCents)
	}
	// A transfer's category lives on the outflow (spending) leg. When editing
	// the inflow leg, surface the pair's category so it's visible and editable.
	if t.TransferPairID != nil && t.CategoryID == nil {
		if pair, err := h.store.GetTransaction(ctx, uid, *t.TransferPairID); err == nil {
			d.CategoryID = pair.CategoryID
		}
	}
	if t.Payee != nil {
		d.Payee = *t.Payee
	}
	if t.Notes != nil {
		d.Notes = *t.Notes
	}
	render(c, http.StatusOK, views.TransactionForm(d))
}

func (h *Handlers) TransactionsUpdate(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.upsertTransaction(c, id); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	// A successful update changed balances; refresh the sidebar overview.
	c.Header("HX-Trigger", "accountsChanged")
	// Re-render the full row list (sorted) so a changed date re-orders the row
	// instead of leaving it stranded in its old position.
	h.renderTxRows(c)
}

func (h *Handlers) TransactionsDelete(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteTransaction(ctx, currentUserID(c), id); err != nil {
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
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	t, err := h.store.GetTransaction(ctx, uid, id)
	if err != nil {
		c.String(http.StatusBadRequest, "not toggleable")
		return
	}
	// For a transfer leg SetCleared flips both legs; the paired row updates on
	// the next full render (HTMX partial-sync is handled separately).
	if err := h.store.SetCleared(ctx, uid, id, !t.Cleared); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	t.Cleared = !t.Cleared
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	render(c, http.StatusOK, views.TransactionRow(*t, accts, cats, false))
}

// upsertTransaction creates (id==0) or updates a transaction or transfer
// from the form fields. Mirrors the TUI's parsing rules.
func (h *Handlers) upsertTransaction(c *gin.Context, id int64) error {
	ctx := c.Request.Context()
	uid := currentUserID(c)
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

	// The form posts a single amount plus a type; income routes it to inflow,
	// expense and transfer to outflow (a transfer is an outflow from the chosen
	// account to the destination). transfer_to is only read for the transfer
	// type, so a stale hidden value on an expense/income is ignored.
	txType := c.PostForm("tx_type")
	if txType == "" {
		txType = "expense"
	}
	var amount int64
	if v := c.PostForm("amount"); v != "" {
		amount, _ = money.Parse(v)
	}
	var outCents, inCents int64
	if txType == "income" {
		inCents = amount
	} else {
		outCents = amount
	}
	var transferTo *int64
	if txType == "transfer" {
		if v := c.PostForm("transfer_to"); v != "" {
			if tid, err := strconv.ParseInt(v, 10, 64); err == nil {
				transferTo = &tid
			}
		}
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

	// Editing can't convert between a transfer and a regular transaction — the
	// user must delete and recreate. Enforce it server-side (the form also
	// disables the mismatched type buttons). An existing transfer leg is then
	// updated in place so its ids and pair linkage survive.
	if id != 0 {
		existing, err := h.store.GetTransaction(ctx, uid, id)
		if err != nil {
			return err
		}
		wasTransfer := existing.TransferPairID != nil
		if wasTransfer != (txType == "transfer") {
			return errInvalid("to switch between a transfer and a regular transaction, delete this one and create a new one")
		}
		if wasTransfer {
			if transferTo == nil {
				return errInvalid("transfer requires a destination account")
			}
			if amount <= 0 {
				return errInvalid("transfer amount required")
			}
			return h.store.UpdateTransfer(ctx, uid, id, store.TransferLegEdit{
				Date: t, AccountID: acctID, TransferAccountID: *transferTo,
				OutflowCents: outCents, InflowCents: inCents,
				CategoryID: catPtr, Notes: notesPtr,
			})
		}
	}

	// Creating a transfer: the amount is an outflow from the chosen account to
	// the destination (the new single-amount form never posts the inflow leg).
	if transferTo != nil {
		if amount <= 0 {
			return errInvalid("transfer amount required")
		}
		_, _, err := h.store.CreateTransfer(ctx, uid, store.TransferInput{
			Date: t, FromAccountID: acctID, ToAccountID: *transferTo,
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
		_, err := h.store.CreateTransaction(ctx, uid, tx)
		return err
	}
	return h.store.UpdateTransaction(ctx, uid, tx)
}

// renderTxRows writes the row HTML for <tbody id="tx-rows"> followed by
// an out-of-band swap that empties the #modal container — so the modal
// form disappears after a successful save.
// Filters are read from the HX-Current-URL header so the active account
// and month filters are preserved after create/update.
func (h *Handlers) renderTxRows(c *gin.Context) {
	ctx := c.Request.Context()

	q := currentURLQuery(c)
	acctPtr := parseIDQuery(q.Get("account"))
	catPtr := parseIDQuery(q.Get("category"))
	month := store.MonthKey(time.Now())
	if m := q.Get("month"); m != "" {
		month = m
	} else if q.Get("all") == "1" {
		month = ""
	}

	uid := currentUserID(c)
	rows, _ := h.store.ListTransactions(ctx, uid, store.TxFilter{
		AccountID: acctPtr, CategoryID: catPtr, Month: month, Limit: txPageSize,
	})
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	c.Status(http.StatusOK)
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = views.TxRows(rows, accts, cats).Render(ctx, c.Writer)
	_, _ = c.Writer.WriteString(`<div id="modal" class="modal-mount" hx-swap-oob="true"></div>`)
}

// txAccountFromRequest returns the account filter reflected by the current
// transactions view (from the HX-Current-URL header), or nil when no account
// filter is active. Used to default a new transaction's account to the
// filtered one.
func txAccountFromRequest(c *gin.Context) *int64 {
	return txFilterFromRequest(c, "account")
}

// txFilterFromRequest reads one id-valued filter (account, category) from the
// query string of the transactions view the request came from.
func txFilterFromRequest(c *gin.Context, param string) *int64 {
	return parseIDQuery(currentURLQuery(c).Get(param))
}

// currentURLQuery returns the query string of the page an HTMX request came
// from, via the HX-Current-URL header. Fragments fetched separately from the
// page (the sidebar overview, the transaction form) have no query string of
// their own, so this is how they see the active filters. Returns nil when the
// header is absent or unparseable; url.Values.Get is nil-safe, so callers can
// read from it unconditionally.
func currentURLQuery(c *gin.Context) url.Values {
	currentURL := c.GetHeader("HX-Current-URL")
	if currentURL == "" {
		return nil
	}
	u, err := url.Parse(currentURL)
	if err != nil {
		return nil
	}
	return u.Query()
}

// parseIDQuery converts a query-string id to a pointer, returning nil when the
// value is absent or unparseable (an unusable filter is treated as no filter).
func parseIDQuery(s string) *int64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

type errInvalid string

func (e errInvalid) Error() string { return string(e) }
