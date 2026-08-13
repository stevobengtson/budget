package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/plaid"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// PlaidLinkStart opens the Plaid Link modal. With ?account=<id> the session is
// linking that existing account to a bank; without it, the user is creating
// new accounts from their bank.
func (h *Handlers) PlaidLinkStart(c *gin.Context) {
	ctx := c.Request.Context()
	token, err := h.plaid.CreateLinkToken(ctx, currentUserID(c), i18n.LocaleFrom(ctx), "")
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusServiceUnavailable, "%s", i18n.T(ctx, "plaid.err_link_start"))
		return
	}
	render(c, http.StatusOK, views.PlaidLinkModal(views.PlaidLinkData{
		LinkToken: token,
		PostURL:   "/plaid/exchange",
		AccountID: parseIDQuery(c.Query("account")),
	}))
}

// PlaidExchange swaps the Link public token for an item (encrypting the access
// token) and moves straight to the account-mapping form. Public tokens expire
// within minutes, so the exchange happens here, not in a later step.
func (h *Handlers) PlaidExchange(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	publicToken := c.PostForm("public_token")
	if publicToken == "" {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "plaid.err_exchange"))
		return
	}
	itemID, err := h.plaid.ExchangePublicToken(ctx, uid, publicToken,
		c.PostForm("institution_id"), c.PostForm("institution_name"))
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusBadGateway, "%s", i18n.T(ctx, "plaid.err_exchange"))
		return
	}
	h.renderPlaidMapForm(c, itemID, parseIDQuery(c.PostForm("account")))
}

// PlaidMapForm re-renders the mapping form (the exchange handler renders it
// directly on success; this route serves reloads and validation retries).
func (h *Handlers) PlaidMapForm(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	h.renderPlaidMapForm(c, id, parseIDQuery(c.Query("account")))
}

func (h *Handlers) renderPlaidMapForm(c *gin.Context, itemID int64, preselect *int64) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	item, err := h.store.GetPlaidItem(ctx, uid, itemID)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	banks, err := h.plaid.ItemAccounts(ctx, uid, itemID)
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusBadGateway, "%s", i18n.T(ctx, "plaid.err_accounts"))
		return
	}
	accts, _ := h.store.ListAccounts(ctx, uid, false)
	d := views.PlaidMapData{
		ItemID:          itemID,
		InstitutionName: item.InstitutionName,
		Preselect:       preselect,
	}
	for _, a := range accts {
		if !a.PlaidLinked() {
			d.Accounts = append(d.Accounts, a)
		}
	}
	for _, b := range banks {
		d.Banks = append(d.Banks, views.PlaidBankRow{
			BankAccount:     b,
			CurrencyAllowed: h.plaid.CurrencyAllowed(b.Currency),
		})
	}
	render(c, http.StatusOK, views.PlaidMapForm(d))
}

// PlaidMapComplete applies the user's mapping choices: per bank account,
// create a new app account, link an existing one, or skip. It then runs the
// item's initial sync synchronously and anchors created accounts' starting
// balances so the derived balance matches the bank.
func (h *Handlers) PlaidMapComplete(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	itemID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetPlaidItem(ctx, uid, itemID)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	banks, err := h.plaid.ItemAccounts(ctx, uid, itemID)
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusBadGateway, "%s", i18n.T(ctx, "plaid.err_accounts"))
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	type anchor struct {
		accountID int64
		bank      plaid.BankAccount
	}
	var created []anchor
	linkedAny := false
	for _, b := range banks {
		choice := c.PostForm("map_" + b.PlaidAccountID)
		if choice == "" || choice == "skip" {
			continue
		}
		if !h.plaid.CurrencyAllowed(b.Currency) {
			c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "plaid.map_currency_blocked"))
			return
		}
		if choice == "new" {
			// New accounts import the full backfill window; the starting balance
			// is anchored after that import lands (see below).
			acctID, err := h.store.CreateAccount(ctx, uid, store.Account{
				Name: bankAccountName(b), Type: bankAccountType(b),
			})
			if err != nil {
				writeStoreErr(c, err)
				return
			}
			syncFrom := h.plaid.SyncFromForNewAccount(today)
			if err := h.store.LinkAccountToPlaid(ctx, uid, acctID, itemID, b.PlaidAccountID, syncFrom); err != nil {
				writeStoreErr(c, err)
				return
			}
			created = append(created, anchor{accountID: acctID, bank: b})
			linkedAny = true
			continue
		}
		// Link an existing account: no history import (syncFrom = today), so the
		// account's tuned starting balance keeps its meaning.
		acctID, err := strconv.ParseInt(choice, 10, 64)
		if err != nil {
			continue
		}
		if err := h.store.LinkAccountToPlaid(ctx, uid, acctID, itemID, b.PlaidAccountID, today); err != nil {
			writeStoreErr(c, err)
			return
		}
		linkedAny = true
	}

	if linkedAny {
		// Initial sync runs synchronously: the created accounts' starting-balance
		// anchor needs the imported net, and the user expects to see transactions
		// when the modal closes.
		if err := h.plaid.SyncItem(ctx, item); err != nil {
			_ = c.Error(err)
			c.String(http.StatusBadGateway, "%s", i18n.T(ctx, "plaid.err_sync"))
			return
		}
		for _, ca := range created {
			h.anchorStartingBalance(c, uid, ca.accountID, ca.bank)
		}
	}

	c.Header("HX-Trigger", "accountsChanged")
	render(c, http.StatusOK, views.PlaidLinkDone())
}

// anchorStartingBalance sets a created account's starting balance so that
// starting + imported net equals the bank's reported balance. Liabilities
// store owed amounts negative (the app's sign convention), while Plaid
// reports credit/loan balances positive-when-owed, hence the flip.
func (h *Handlers) anchorStartingBalance(c *gin.Context, uid, accountID int64, b plaid.BankAccount) {
	ctx := c.Request.Context()
	if !b.HasBalance {
		return
	}
	accts, err := h.store.ListAccounts(ctx, uid, true)
	if err != nil {
		return
	}
	for _, a := range accts {
		if a.ID != accountID {
			continue
		}
		target := b.BalanceCents
		if a.Type.IsLiability() {
			target = -target
		}
		// The account was created with starting balance 0, so its current derived
		// balance IS the imported net.
		acct := a.Account
		acct.StartingBalanceCents = target - a.BalanceCents
		_ = h.store.UpdateAccount(ctx, uid, acct)
		return
	}
}

// PlaidReconnect opens Link in update mode for an item that needs
// re-authentication (login_required / pending_expiration).
func (h *Handlers) PlaidReconnect(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	itemID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	token, err := h.plaid.CreateUpdateLinkToken(ctx, uid, itemID, i18n.LocaleFrom(ctx))
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusServiceUnavailable, "%s", i18n.T(ctx, "plaid.err_link_start"))
		return
	}
	render(c, http.StatusOK, views.PlaidLinkModal(views.PlaidLinkData{
		LinkToken: token,
		PostURL:   "/plaid/items/" + strconv.FormatInt(itemID, 10) + "/reconnected",
	}))
}

// PlaidReconnected marks a re-authenticated item healthy again and syncs it.
// Update mode reuses the existing access token, so there is nothing to
// exchange — Link succeeding is the news.
func (h *Handlers) PlaidReconnected(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	itemID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetPlaidItem(ctx, uid, itemID)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	if err := h.store.SetPlaidItemStatus(ctx, item.ID, store.PlaidItemActive, ""); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	item.Status = store.PlaidItemActive
	_ = h.plaid.SyncItem(ctx, item)
	c.Header("HX-Trigger", "accountsChanged")
	render(c, http.StatusOK, views.PlaidLinkDone())
}

// PlaidUnlinkAccount disconnects one account from its bank. When it was the
// item's last linked account the item itself is removed at Plaid, so the
// access token does not outlive the user's intent. Imported transactions stay.
func (h *Handlers) PlaidUnlinkAccount(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	acctID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	a, err := h.store.GetAccount(ctx, uid, acctID)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	if !a.PlaidLinked() {
		c.Header("HX-Trigger", "accountsChanged")
		c.Writer.WriteHeader(http.StatusOK)
		return
	}
	itemID := *a.PlaidItemID
	if err := h.store.UnlinkAccountFromPlaid(ctx, uid, acctID); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if n, err := h.store.CountAccountsForPlaidItem(ctx, uid, itemID); err == nil && n == 0 {
		_ = h.plaid.RemoveItem(ctx, uid, itemID)
	}
	c.Header("HX-Trigger", "accountsChanged")
	c.Writer.WriteHeader(http.StatusOK)
}

// PlaidWebhook receives Plaid webhook events. The Plaid-Verification JWT
// (checked inside plaid.HandleWebhook) authenticates the request — there is no
// session. Verification failure is a 401; handled or ignored events are 200,
// so Plaid does not retry events this app deliberately skips.
func (h *Handlers) PlaidWebhook(c *gin.Context) {
	if !h.plaid.Enabled() {
		c.Status(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBytes))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	err = h.plaid.HandleWebhook(c.Request.Context(), body, c.GetHeader("Plaid-Verification"), h.billing.BankSyncEntitled)
	switch {
	case errors.Is(err, plaid.ErrWebhookVerification):
		c.Status(http.StatusUnauthorized)
	case err != nil:
		_ = c.Error(err)
		c.Status(http.StatusBadRequest)
	default:
		c.Status(http.StatusOK)
	}
}

// bankAccountName builds the created account's name from the bank's own
// label, disambiguated with the mask when present ("Chequing ••1234").
func bankAccountName(b plaid.BankAccount) string {
	if b.Mask != "" {
		return b.Name + " ••" + b.Mask
	}
	return b.Name
}

// bankAccountType maps Plaid's account taxonomy onto the app's: credit and
// loan map directly, savings-subtype depositories are savings, and everything
// else (chequing, investment, other) lands on checking — the user can edit
// the type afterwards.
func bankAccountType(b plaid.BankAccount) store.AccountType {
	switch b.Type {
	case "credit":
		return store.TypeCredit
	case "loan":
		return store.TypeLoan
	case "depository":
		if b.Subtype == "savings" {
			return store.TypeSavings
		}
	}
	return store.TypeChecking
}
