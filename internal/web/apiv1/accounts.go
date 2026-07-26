package apiv1

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
)

type accountItem struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	BalanceCents int64  `json:"balanceCents"`
}

// Accounts lists the user's active (non-archived) accounts with balances as of
// the end of the selected month (default: current). Returns month-nav keys so
// the list shares the app-wide month selector. Tapping an account drills into
// its transactions (the account-scoped model).
func (a *API) Accounts(c *gin.Context) {
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	} else if !validMonth(month) {
		writeError(c, http.StatusBadRequest, "invalid_request", "month must be formatted YYYY-MM.")
		return
	}

	accts, err := a.store.ListAccountsAsOf(c.Request.Context(), c.GetInt64(contextUserID), false, month)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load accounts.")
		return
	}
	out := make([]accountItem, 0, len(accts))
	for _, ac := range accts {
		out = append(out, accountItem{
			ID:           ac.ID,
			Name:         ac.Name,
			Type:         string(ac.Type),
			BalanceCents: ac.BalanceCents,
		})
	}

	parsed, _ := time.Parse("2006-01", month)
	c.JSON(http.StatusOK, gin.H{
		"month":     month,
		"prevMonth": store.PrevMonth(month),
		"nextMonth": parsed.AddDate(0, 1, 0).Format("2006-01"),
		"accounts":  out,
	})
}

type txItem struct {
	ID          int64  `json:"id"`
	Date        string `json:"date"` // YYYY-MM-DD
	Payee       string `json:"payee"`
	Category    string `json:"category"`
	CategoryID  *int64 `json:"categoryId"` // for edit prefill; null = uncategorized
	Notes       string `json:"notes"`
	AmountCents int64  `json:"amountCents"` // inflow − outflow (positive = money in)
	Cleared     bool   `json:"cleared"`
	IsTransfer  bool   `json:"isTransfer"` // transfers can't be edited in the app
}

// txPageLimit caps how many recent transactions a single account view returns.
const txPageLimit = 200

// AccountTransactions returns the account's recent transactions, newest first.
// Category ids are resolved to names here so the client renders labels directly.
func (a *API) AccountTransactions(c *gin.Context) {
	ctx := c.Request.Context()
	uid := c.GetInt64(contextUserID)

	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Invalid account id.")
		return
	}

	// Confirm the account is the user's (GetAccount is user-scoped).
	if _, err := a.store.GetAccount(ctx, uid, accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(c, http.StatusNotFound, "not_found", "That account doesn't exist.")
			return
		}
		writeError(c, http.StatusInternalServerError, "internal", "Could not load the account.")
		return
	}

	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	} else if !validMonth(month) {
		writeError(c, http.StatusBadRequest, "invalid_request", "month must be formatted YYYY-MM.")
		return
	}

	txs, err := a.store.ListTransactions(ctx, uid, store.TxFilter{AccountID: &accountID, Month: month, Limit: txPageLimit})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load transactions.")
		return
	}

	// Resolve category names (include archived so historical categories resolve).
	cats, _ := a.store.ListCategories(ctx, uid, true)
	names := make(map[int64]string, len(cats))
	for _, cat := range cats {
		names[cat.ID] = cat.Name
	}

	out := make([]txItem, 0, len(txs))
	for _, t := range txs {
		item := txItem{
			ID:          t.ID,
			Date:        t.Date.Format("2006-01-02"),
			CategoryID:  t.CategoryID,
			AmountCents: t.InflowCents - t.OutflowCents,
			Cleared:     t.Cleared,
			IsTransfer:  t.TransferAccountID != nil || t.TransferPairID != nil,
		}
		if t.Payee != nil {
			item.Payee = *t.Payee
		}
		if t.Notes != nil {
			item.Notes = *t.Notes
		}
		if t.CategoryID != nil {
			item.Category = names[*t.CategoryID]
		}
		out = append(out, item)
	}

	parsed, _ := time.Parse("2006-01", month)
	c.JSON(http.StatusOK, gin.H{
		"month":        month,
		"prevMonth":    store.PrevMonth(month),
		"nextMonth":    parsed.AddDate(0, 1, 0).Format("2006-01"),
		"transactions": out,
	})
}

type createTxRequest struct {
	Date       string `json:"date"`       // optional YYYY-MM-DD; default today
	Amount     string `json:"amount"`     // human string, parsed via money.Parse
	Type       string `json:"type"`       // "expense" (default) or "income"
	CategoryID *int64 `json:"categoryId"` // optional; omit/null = uncategorized
	Payee      string `json:"payee"`
	Notes      string `json:"notes"`
}

// CreateTransaction adds an expense or income transaction to an account.
// Transfers (which move between two accounts) are intentionally not handled
// here. CreateTransaction in the store guards account + category ownership.
func (a *API) CreateTransaction(c *gin.Context) {
	ctx := c.Request.Context()
	uid := c.GetInt64(contextUserID)

	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Invalid account id.")
		return
	}

	var req createTxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with amount and type.")
		return
	}

	date := time.Now()
	if req.Date != "" {
		d, err := time.Parse("2006-01-02", req.Date)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "date must be formatted YYYY-MM-DD.")
			return
		}
		date = d
	}

	cents, err := money.Parse(req.Amount)
	if err != nil || cents <= 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Enter an amount greater than zero.")
		return
	}

	var inflow, outflow int64
	switch req.Type {
	case "income":
		inflow = cents
	case "expense", "":
		outflow = cents
	default:
		writeError(c, http.StatusBadRequest, "invalid_request", "type must be expense or income.")
		return
	}

	tx := store.Transaction{
		Date:         date,
		AccountID:    accountID,
		CategoryID:   req.CategoryID,
		OutflowCents: outflow,
		InflowCents:  inflow,
	}
	if req.Payee != "" {
		tx.Payee = &req.Payee
	}
	if req.Notes != "" {
		tx.Notes = &req.Notes
	}

	id, err := a.store.CreateTransaction(ctx, uid, tx)
	if err != nil {
		if errors.Is(err, store.ErrNotOwned) {
			writeError(c, http.StatusNotFound, "not_found", "That account or category doesn't exist.")
			return
		}
		writeError(c, http.StatusInternalServerError, "internal", "Could not save the transaction.")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// UpdateTransaction edits an existing expense or income transaction in place.
// The account can't change (the app is account-scoped) and the cleared state is
// preserved, since the app's form doesn't expose either. Transfers are refused —
// the client hides the edit affordance on them, and this is the server guard.
func (a *API) UpdateTransaction(c *gin.Context) {
	ctx := c.Request.Context()
	uid := c.GetInt64(contextUserID)

	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Invalid account id.")
		return
	}
	txID, err := strconv.ParseInt(c.Param("txId"), 10, 64)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Invalid transaction id.")
		return
	}

	// Load the existing row (user-scoped) to confirm ownership, keep the fields
	// the form doesn't edit (cleared), and reject transfers.
	existing, err := a.store.GetTransaction(ctx, uid, txID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(c, http.StatusNotFound, "not_found", "That transaction doesn't exist.")
			return
		}
		writeError(c, http.StatusInternalServerError, "internal", "Could not load the transaction.")
		return
	}
	if existing.AccountID != accountID {
		writeError(c, http.StatusNotFound, "not_found", "That transaction doesn't exist.")
		return
	}
	if existing.TransferAccountID != nil || existing.TransferPairID != nil {
		writeError(c, http.StatusUnprocessableEntity, "unsupported", "Transfers can't be edited in the app yet.")
		return
	}

	var req createTxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with amount and type.")
		return
	}

	date := existing.Date
	if req.Date != "" {
		d, err := time.Parse("2006-01-02", req.Date)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "date must be formatted YYYY-MM-DD.")
			return
		}
		date = d
	}

	cents, err := money.Parse(req.Amount)
	if err != nil || cents <= 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Enter an amount greater than zero.")
		return
	}

	var inflow, outflow int64
	switch req.Type {
	case "income":
		inflow = cents
	case "expense", "":
		outflow = cents
	default:
		writeError(c, http.StatusBadRequest, "invalid_request", "type must be expense or income.")
		return
	}

	tx := store.Transaction{
		ID:           txID,
		Date:         date,
		AccountID:    accountID,
		CategoryID:   req.CategoryID,
		OutflowCents: outflow,
		InflowCents:  inflow,
		Cleared:      existing.Cleared,
	}
	if req.Payee != "" {
		tx.Payee = &req.Payee
	}
	if req.Notes != "" {
		tx.Notes = &req.Notes
	}

	if err := a.store.UpdateTransaction(ctx, uid, tx); err != nil {
		if errors.Is(err, store.ErrNotOwned) {
			writeError(c, http.StatusNotFound, "not_found", "That account or category doesn't exist.")
			return
		}
		writeError(c, http.StatusInternalServerError, "internal", "Could not save the transaction.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": txID})
}
