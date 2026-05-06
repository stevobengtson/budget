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
	render(c, http.StatusOK, views.TransactionForm(d))
}

func (h *Handlers) TransactionsCreate(c *gin.Context) {
	if err := h.upsertTransaction(c, 0); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	// Re-render full row list scoped by current filters.
	h.renderTxRows(c)
}

func (h *Handlers) TransactionsEdit(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	all, _ := h.store.ListTransactions(ctx, store.TxFilter{Limit: 100000})
	var t *store.Transaction
	for i := range all {
		if all[i].ID == id {
			t = &all[i]
			break
		}
	}
	if t == nil {
		c.String(http.StatusNotFound, "tx not found")
		return
	}
	if t.TransferPairID != nil {
		c.String(http.StatusBadRequest, "transfers are not editable; delete and recreate")
		return
	}
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	d := views.TxFormData{
		Editing:    true,
		ID:         t.ID,
		Date:       t.Date.Format("2006-01-02"),
		AccountID:  t.AccountID,
		CategoryID: t.CategoryID,
		Accounts:   accts,
		Categories: cats,
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
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.upsertTransaction(c, id); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	h.renderTxRows(c)
}

func (h *Handlers) TransactionsDelete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteTransaction(c.Request.Context(), id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) TransactionsToggleCleared(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	all, _ := h.store.ListTransactions(ctx, store.TxFilter{Limit: 100000})
	var t *store.Transaction
	for i := range all {
		if all[i].ID == id {
			t = &all[i]
			break
		}
	}
	if t == nil || t.TransferPairID != nil {
		c.String(http.StatusBadRequest, "not toggleable")
		return
	}
	t.Cleared = !t.Cleared
	if err := h.store.UpdateTransaction(ctx, *t); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	render(c, http.StatusOK, views.TransactionRow(*t, accts, cats))
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
// an out-of-band swap that empties the #tx-form container — so the
// inline new/edit form disappears after a successful save.
func (h *Handlers) renderTxRows(c *gin.Context) {
	ctx := c.Request.Context()
	rows, _ := h.store.ListTransactions(ctx, store.TxFilter{Limit: txPageSize})
	accts, _ := h.store.ListAccounts(ctx, true)
	cats, _ := h.store.ListCategories(ctx, true)
	c.Status(http.StatusOK)
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, t := range rows {
		_ = views.TransactionRow(t, accts, cats).Render(ctx, c.Writer)
	}
	_, _ = c.Writer.WriteString(`<div id="modal" class="modal-mount" hx-swap-oob="true"></div>`)
}

type errInvalid string

func (e errInvalid) Error() string { return string(e) }
