package plaid

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	plaidapi "github.com/plaid/plaid-go/v45/plaid"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
)

// clientName is what Plaid Link shows as the requesting application.
const clientName = "Pigglet Budget"

// BankAccount is one bank account within an item, as needed by the mapping
// step: identity, display fields, and the current balance in cents for the
// starting-balance math. Currency gates linking (the app is single-currency).
type BankAccount struct {
	PlaidAccountID string
	Name           string
	Mask           string
	Type           string // Plaid type: depository / credit / loan / investment / other
	Subtype        string
	Currency       string // ISO-4217, e.g. "CAD"
	BalanceCents   int64
	HasBalance     bool
}

// CreateLinkToken starts a Plaid Link session for the user. accessToken must
// be "" for a fresh link; passing an item's decrypted token switches Link to
// update mode (re-authentication after ITEM_LOGIN_REQUIRED), where products
// are omitted per the API contract.
func (s *Service) CreateLinkToken(ctx context.Context, userID int64, locale i18n.Locale, accessToken string) (string, error) {
	if !s.configured {
		return "", ErrNotConfigured
	}
	lang, _, _ := strings.Cut(string(locale), "-")
	if lang == "" {
		lang = "en"
	}
	req := plaidapi.NewLinkTokenCreateRequest(
		clientName, lang,
		[]plaidapi.CountryCode{plaidapi.COUNTRYCODE_CA, plaidapi.COUNTRYCODE_US},
	)
	req.SetUser(*plaidapi.NewLinkTokenCreateRequestUser(strconv.FormatInt(userID, 10)))
	req.SetWebhook(s.webhookURL)
	if accessToken != "" {
		req.SetAccessToken(accessToken)
	} else {
		req.SetProducts([]plaidapi.Products{plaidapi.PRODUCTS_TRANSACTIONS})
		days := int32(s.days)
		req.SetTransactions(plaidapi.LinkTokenTransactions{DaysRequested: &days})
	}
	resp, err := s.api.LinkTokenCreate(ctx, *req)
	if err != nil {
		return "", fmt.Errorf("create link token: %s", errorCode(err))
	}
	return resp.GetLinkToken(), nil
}

// CreateUpdateLinkToken opens Link in update mode for one of the user's
// existing items (re-auth after login_required / pending_expiration).
func (s *Service) CreateUpdateLinkToken(ctx context.Context, userID, itemID int64, locale i18n.Locale) (string, error) {
	if !s.configured {
		return "", ErrNotConfigured
	}
	item, err := s.store.GetPlaidItem(ctx, userID, itemID)
	if err != nil {
		return "", err
	}
	token, err := s.sealer.Open(item.AccessTokenEncrypted)
	if err != nil {
		return "", fmt.Errorf("unseal access token for item %d: %w", item.ID, err)
	}
	return s.CreateLinkToken(ctx, userID, locale, token)
}

// ExchangePublicToken swaps a Link public token for the item's access token,
// seals it, and stores the item. The access token exists in plaintext only
// inside this function — it must never reach an error, a log line, or the
// caller.
func (s *Service) ExchangePublicToken(ctx context.Context, userID int64, publicToken, institutionID, institutionName string) (int64, error) {
	if !s.configured {
		return 0, ErrNotConfigured
	}
	resp, err := s.api.ItemPublicTokenExchange(ctx, publicToken)
	if err != nil {
		return 0, fmt.Errorf("exchange public token: %s", errorCode(err))
	}
	sealed, err := s.sealer.Seal(resp.GetAccessToken())
	if err != nil {
		return 0, fmt.Errorf("seal access token: %w", err)
	}
	id, err := s.store.CreatePlaidItem(ctx, store.PlaidItem{
		UserID:               userID,
		PlaidItemID:          resp.GetItemId(),
		AccessTokenEncrypted: sealed,
		InstitutionID:        institutionID,
		InstitutionName:      institutionName,
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ItemAccounts lists the bank accounts inside one of the user's items, for the
// account-mapping form.
func (s *Service) ItemAccounts(ctx context.Context, userID, itemID int64) ([]BankAccount, error) {
	if !s.configured {
		return nil, ErrNotConfigured
	}
	item, err := s.store.GetPlaidItem(ctx, userID, itemID)
	if err != nil {
		return nil, err
	}
	token, err := s.sealer.Open(item.AccessTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("unseal access token for item %d: %w", item.ID, err)
	}
	resp, err := s.api.AccountsGet(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("accounts get: %s", errorCode(err))
	}
	out := make([]BankAccount, 0, len(resp.GetAccounts()))
	for _, a := range resp.GetAccounts() {
		b := BankAccount{
			PlaidAccountID: a.GetAccountId(),
			Name:           a.GetName(),
			Mask:           a.GetMask(),
			Type:           string(a.GetType()),
			Subtype:        string(a.GetSubtype()),
			Currency:       a.Balances.GetIsoCurrencyCode(),
		}
		if cur, ok := a.Balances.GetCurrentOk(); ok && cur != nil {
			b.BalanceCents = toCents(*cur)
			b.HasBalance = true
		}
		out = append(out, b)
	}
	return out, nil
}

// RemoveItem revokes the item's access token at Plaid and deletes the local
// row plus any account links. The local delete proceeds even when Plaid
// reports the item already gone — the goal is that no orphaned token survives
// on either side.
func (s *Service) RemoveItem(ctx context.Context, userID, itemID int64) error {
	if !s.configured {
		return ErrNotConfigured
	}
	item, err := s.store.GetPlaidItem(ctx, userID, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if token, err := s.sealer.Open(item.AccessTokenEncrypted); err == nil {
		if err := s.api.ItemRemove(ctx, token); err != nil {
			// Best-effort at Plaid: ITEM_NOT_FOUND (already removed) is fine, and
			// any other failure must not strand the user with an unremovable link.
			// The code alone is loggable; the error itself is not.
			logSyncWarn("item remove at plaid failed", item.ID, errorCode(err))
		}
	}
	for _, a := range mustListLinked(ctx, s.store, item.ID) {
		_ = s.store.UnlinkAccountFromPlaid(ctx, userID, a.ID)
	}
	return s.store.DeletePlaidItem(ctx, userID, itemID)
}

// RemoveAllItemsForUser revokes and deletes every item the user has — the
// account-deletion and start-fresh paths, where tokens must not outlive the
// user's data. Best-effort per item; the store wipe that follows removes any
// rows a failed removal leaves behind.
func (s *Service) RemoveAllItemsForUser(ctx context.Context, userID int64) {
	if !s.configured {
		return
	}
	items, err := s.store.ListPlaidItemsForUser(ctx, userID)
	if err != nil {
		return
	}
	for _, it := range items {
		_ = s.RemoveItem(ctx, userID, it.ID)
	}
}

// mustListLinked is ListLinkedAccounts with the error swallowed, for cleanup
// paths that should proceed regardless.
func mustListLinked(ctx context.Context, st *store.Store, itemID int64) []store.Account {
	accts, _ := st.ListLinkedAccounts(ctx, itemID)
	return accts
}

// toCents converts a Plaid float amount to integer cents without truncation
// drift (Plaid amounts are decimal dollars).
func toCents(amount float64) int64 {
	if amount < 0 {
		return -toCents(-amount)
	}
	return int64(amount*100 + 0.5)
}

// SyncFromForNewAccount is today minus the configured history window: a
// newly created account imports the full requested backfill.
func (s *Service) SyncFromForNewAccount(now time.Time) time.Time {
	return now.AddDate(0, 0, -s.days)
}
