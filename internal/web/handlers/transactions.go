package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	reviewOnly := c.Query("review") == "1"

	uid := currentUserID(c)
	rows, err := h.store.ListTransactions(ctx, uid, store.TxFilter{
		AccountID: acctPtr, CategoryID: catPtr, Month: month, NeedsReview: reviewOnly, Limit: 5000,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// Collapse each transfer's two stored legs to the one that will be rendered
	// BEFORE paginating, so the pager counts rows the user can actually see.
	rows = views.VisibleTxRows(rows, acctPtr)
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
		FilterReview:   reviewOnly,
		ReviewCount:    views.ReviewCountFrom(ctx),
		PrevMonth:      prev,
		NextMonth:      next,
		Today:          store.MonthKey(time.Now()),
		Page:           page,
		PageSize:       txPageSize,
		Total:          total,
	}
	render(c, http.StatusOK, views.TransactionsPage(data, sidebarCollapsed(c)))
}

func (h *Handlers) TransactionsCreate(c *gin.Context) {
	if err := h.upsertTransaction(c, 0); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Header("HX-Trigger", "accountsChanged")
	// Re-render full row list scoped by current filters, plus a fresh quick-add
	// row so the entry that just saved leaves the form empty.
	h.renderTxRows(c, true)
}

// txFormData maps a stored transaction onto the single (type, amount) shape both
// editors use. Shared by the side sheet and the inline transfer editor so the
// two can never disagree about which leg is which.
//
// Transfers are always presented from the source's perspective — account is
// "from", transfer_to is "to", the amount is the outflow — so editing the
// inflow (destination) leg is normalized to keep the amount an outflow.
func (h *Handlers) txFormData(ctx context.Context, uid int64, t *store.Transaction) views.TxFormData {
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	groups, _ := h.store.ListGroups(ctx, uid)
	d := views.TxFormData{
		Editing:    true,
		ID:         t.ID,
		Date:       t.Date.Format("2006-01-02"),
		AccountID:  t.AccountID,
		CategoryID: t.CategoryID,
		Accounts:   accts,
		Categories: cats,
		Groups:     groups,
	}
	switch {
	case t.TransferAccountID != nil:
		d.TxType = "transfer"
		if t.OutflowCents > 0 {
			d.TransferAccountID = t.TransferAccountID
			d.Amount = money.Format(ctx, t.OutflowCents)
		} else {
			src := *t.TransferAccountID
			dst := t.AccountID
			d.AccountID = src
			d.TransferAccountID = &dst
			d.Amount = money.Format(ctx, t.InflowCents)
		}
	case t.InflowCents > 0:
		d.TxType = "income"
		d.Amount = money.Format(ctx, t.InflowCents)
	default:
		d.TxType = "expense"
		d.Amount = money.Format(ctx, t.OutflowCents)
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
	return d
}

// TransactionsEditRow renders the in-place editor that replaces a row. Every
// transaction type edits this way: a transfer needs it (one record spanning two
// accounts), and one editing idiom beats two in the same list.
func (h *Handlers) TransactionsEditRow(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	tx, err := h.store.GetTransaction(ctx, uid, id)
	if err != nil {
		c.String(http.StatusNotFound, "tx not found")
		return
	}
	render(c, http.StatusOK, views.TransactionEditRow(h.txFormData(ctx, uid, tx)))
}

// TransactionsRow re-renders a single row. It is what Cancel in the inline
// editor swaps back, so leaving an edit costs no page reload.
func (h *Handlers) TransactionsRow(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	tx, err := h.store.GetTransaction(ctx, uid, id)
	if err != nil {
		c.String(http.StatusNotFound, "tx not found")
		return
	}
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	render(c, http.StatusOK, views.TransactionRow(*tx, accts, cats, false))
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
	// instead of leaving it stranded in its old position. The quick-add row is
	// left alone: an edit comes from the sheet, and anything half-typed in the
	// entry row above should survive it.
	h.renderTxRows(c, false)
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

// TransactionsMarkReviewed clears one import's needs-review flag and re-renders
// the row (the amber tag doubles as the action).
func (h *Handlers) TransactionsMarkReviewed(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.SetTransactionReviewed(ctx, uid, id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	t, err := h.store.GetTransaction(ctx, uid, id)
	if err != nil {
		c.String(http.StatusNotFound, "tx not found")
		return
	}
	accts, _ := h.store.ListAccounts(ctx, uid, true)
	cats, _ := h.store.ListCategories(ctx, uid, true)
	render(c, http.StatusOK, views.TransactionRow(*t, accts, cats, false))
}

// TransactionsReviewAll clears every needs-review flag within the current
// filter scope (account/category/month from HX-Current-URL) and re-renders the
// row list the same way a create does.
func (h *Handlers) TransactionsReviewAll(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	q := currentURLQuery(c)
	month := store.MonthKey(time.Now())
	if m := q.Get("month"); m != "" {
		month = m
	} else if q.Get("all") == "1" {
		month = ""
	}
	f := store.TxFilter{
		AccountID:  parseIDQuery(q.Get("account")),
		CategoryID: parseIDQuery(q.Get("category")),
		Month:      month,
	}
	if _, err := h.store.MarkAllReviewed(ctx, uid, f); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// The badge count changed and the filtered list may now be empty; a full
	// page refresh via HX-Redirect keeps every affected fragment honest.
	q.Del("review")
	target := "/transactions"
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	c.Header("HX-Redirect", target)
	c.Status(http.StatusOK)
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

	// Both category fields live in the DOM at once — the plain one and the
	// transfer-only one, each shown by CSS for its own transaction type — and a
	// display:none select still posts. They therefore use distinct names, and the
	// transfer field wins when the form is a transfer, so a category left over
	// from expense mode cannot leak onto a transfer leg.
	var catPtr *int64
	catField := "category_id"
	if c.PostForm("tx_type") == "transfer" {
		// Authoritative, not a fallback: an empty transfer category must mean no
		// category, otherwise a value left in the hidden expense field leaks onto
		// the transfer leg.
		catField = "transfer_category_id"
	}
	if v := c.PostForm(catField); v != "" {
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
	// The parse error used to be discarded, so "twelve" quietly became a $0.00
	// transaction. A malformed amount is a mistake to report, not a zero.
	var amount int64
	if v := strings.TrimSpace(c.PostForm("amount")); v != "" {
		parsed, err := money.Parse(ctx, v)
		if err != nil {
			return errInvalid("amount: " + err.Error())
		}
		amount = parsed
	}
	var outCents, inCents int64
	if txType == "income" {
		inCents = amount
	} else {
		outCents = amount
	}
	// A transfer needs somewhere to go. Without this, a transfer missing its
	// destination fell through to the plain-transaction path below and was
	// silently stored as an ordinary expense from the source account.
	var transferTo *int64
	if txType == "transfer" {
		v := strings.TrimSpace(c.PostForm("transfer_to"))
		if v == "" {
			return errInvalid("choose an account for the transfer to go to")
		}
		tid, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return errInvalid("transfer destination is not valid")
		}
		if tid == acctID {
			return errInvalid("a transfer needs two different accounts")
		}
		transferTo = &tid
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
	// Reconciling is a separate action (the row's cleared checkbox -> SetCleared),
	// so an edit must carry the stored flag through: UpdateTransaction writes
	// every column, and the zero value here would silently un-reconcile the row.
	// Left false when creating — a new transaction has not been seen on a
	// statement yet.
	var cleared bool
	if id != 0 {
		existing, err := h.store.GetTransaction(ctx, uid, id)
		if err != nil {
			return err
		}
		cleared = existing.Cleared
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
		Cleared: cleared,
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
//
// resetQuickAdd additionally sends the quick-add row back out of band, so a
// created transaction leaves the entry form at its page defaults. It is off for
// an update, which is edited elsewhere and must not clobber the entry row.
func (h *Handlers) renderTxRows(c *gin.Context, resetQuickAdd bool) {
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
	_ = views.TxRows(rows, accts, cats, acctPtr).Render(ctx, c.Writer)
	if resetQuickAdd {
		// Only the fields the row actually reads — it is being re-rendered for its
		// defaults, not for the list.
		groups, _ := h.store.ListGroups(ctx, uid)
		_ = views.TxQuickAdd(views.TransactionsData{
			Accounts:       accts,
			Categories:     cats,
			Groups:         groups,
			FilterAccount:  acctPtr,
			FilterCategory: catPtr,
		}, true).Render(ctx, c.Writer)
	}
	_, _ = c.Writer.WriteString(`<div id="modal" class="modal-mount" hx-swap-oob="true"></div>`)
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

// currentURLPath is currentURLQuery's counterpart: the path the htmx request
// came from. The accounts overview needs it to tell "viewing all accounts" from
// "not on the transactions page at all" — both have no account filter.
func currentURLPath(c *gin.Context) string {
	currentURL := c.GetHeader("HX-Current-URL")
	if currentURL == "" {
		return ""
	}
	u, err := url.Parse(currentURL)
	if err != nil {
		return ""
	}
	return u.Path
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
