package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Plaid item statuses (mirrors the CHECK constraint on plaid_items.status).
const (
	PlaidItemActive            = "active"
	PlaidItemLoginRequired     = "login_required"
	PlaidItemPendingExpiration = "pending_expiration"
	PlaidItemError             = "error"
)

// PlaidItem is one linked bank login (a Plaid "Item"). AccessTokenEncrypted is
// the AES-GCM sealed access token — only the plaid service ever decrypts it,
// and the plaintext must never appear in logs or errors.
type PlaidItem struct {
	ID                   int64
	UserID               int64
	PlaidItemID          string
	AccessTokenEncrypted []byte
	InstitutionID        string
	InstitutionName      string
	SyncCursor           string
	Status               string
	LastError            string
	LastSyncedAt         *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

const plaidItemColumns = `id, user_id, plaid_item_id, access_token_encrypted, institution_id,
	institution_name, sync_cursor, status, last_error, last_synced_at, created_at, updated_at`

func (s *Store) scanPlaidItem(row interface{ Scan(...any) error }) (PlaidItem, error) {
	var it PlaidItem
	var synced nullTime
	if err := row.Scan(&it.ID, &it.UserID, &it.PlaidItemID, &it.AccessTokenEncrypted,
		&it.InstitutionID, &it.InstitutionName, &it.SyncCursor, &it.Status,
		&it.LastError, &synced, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return PlaidItem{}, err
	}
	it.LastSyncedAt = synced.Ptr()
	return it, nil
}

// CreatePlaidItem stores a freshly exchanged item and returns its row id.
func (s *Store) CreatePlaidItem(ctx context.Context, it PlaidItem) (int64, error) {
	id, err := s.insertReturningID(ctx,
		`INSERT INTO plaid_items(user_id, plaid_item_id, access_token_encrypted, institution_id, institution_name)
		 VALUES ($1, $2, $3, $4, $5)`,
		it.UserID, it.PlaidItemID, it.AccessTokenEncrypted, it.InstitutionID, it.InstitutionName)
	if err != nil {
		return 0, fmt.Errorf("create plaid item: %w", err)
	}
	return id, nil
}

// GetPlaidItem returns one of the user's items by row id.
func (s *Store) GetPlaidItem(ctx context.Context, userID, id int64) (PlaidItem, error) {
	return s.scanPlaidItem(s.queryOne(ctx,
		`SELECT `+plaidItemColumns+` FROM plaid_items WHERE id=$1 AND user_id=$2`, id, userID))
}

// GetPlaidItemByItemID resolves an item by its Plaid item_id — the webhook
// path, where there is no session; the owning user comes from the row itself.
func (s *Store) GetPlaidItemByItemID(ctx context.Context, plaidItemID string) (PlaidItem, error) {
	return s.scanPlaidItem(s.queryOne(ctx,
		`SELECT `+plaidItemColumns+` FROM plaid_items WHERE plaid_item_id=$1`, plaidItemID))
}

// ListPlaidItemsForUser returns the user's items, oldest first.
func (s *Store) ListPlaidItemsForUser(ctx context.Context, userID int64) ([]PlaidItem, error) {
	return s.listPlaidItems(ctx,
		`SELECT `+plaidItemColumns+` FROM plaid_items WHERE user_id=$1 ORDER BY created_at`, userID)
}

// ListActivePlaidItems returns every syncable item across all users (the
// polling worker's work list): status active, not synced since the cutoff.
func (s *Store) ListActivePlaidItems(ctx context.Context, syncedBefore time.Time) ([]PlaidItem, error) {
	return s.listPlaidItems(ctx,
		`SELECT `+plaidItemColumns+` FROM plaid_items
		 WHERE status=$1 AND (last_synced_at IS NULL OR last_synced_at < $2)
		 ORDER BY last_synced_at NULLS FIRST`, PlaidItemActive, syncedBefore)
}

func (s *Store) listPlaidItems(ctx context.Context, q string, args ...any) ([]PlaidItem, error) {
	rows, err := s.queryAll(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list plaid items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PlaidItem
	for rows.Next() {
		it, err := s.scanPlaidItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UpdatePlaidItemCursor persists the item's /transactions/sync cursor and
// stamps last_synced_at. Only called after a fully paginated, fully applied
// sync — an interrupted sync must leave the cursor untouched so the next run
// replays the same window (the unique plaid_transaction_id index makes the
// replay idempotent).
func (s *Store) UpdatePlaidItemCursor(ctx context.Context, id int64, cursor string) error {
	_, err := s.run(ctx,
		`UPDATE plaid_items SET sync_cursor=$1, last_synced_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		 WHERE id=$2`, cursor, id)
	return err
}

// SetPlaidItemStatus records the item's health (and an optional token-free
// error message, truncated to keep pathological API responses out of the row).
func (s *Store) SetPlaidItemStatus(ctx context.Context, id int64, status, lastError string) error {
	const maxErrLen = 500
	if len(lastError) > maxErrLen {
		lastError = lastError[:maxErrLen]
	}
	_, err := s.run(ctx,
		`UPDATE plaid_items SET status=$1, last_error=$2, updated_at=CURRENT_TIMESTAMP WHERE id=$3`,
		status, lastError, id)
	return err
}

// DeletePlaidItem removes the item row. Accounts pointing at it must be
// unlinked first (the FK has no cascade); the caller is responsible for having
// revoked the token at Plaid.
func (s *Store) DeletePlaidItem(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx, `DELETE FROM plaid_items WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// LinkAccountToPlaid points an account at a bank account within one of the
// user's items. syncFrom bounds imports (transactions dated before it are
// skipped), which is what protects an already-balanced existing account from
// double-counting history.
func (s *Store) LinkAccountToPlaid(ctx context.Context, userID, accountID, itemID int64, plaidAccountID string, syncFrom time.Time) error {
	if err := s.requireAccount(ctx, userID, accountID); err != nil {
		return err
	}
	if _, err := s.GetPlaidItem(ctx, userID, itemID); err != nil {
		return ErrNotOwned
	}
	_, err := s.run(ctx,
		`UPDATE accounts SET plaid_item_id=$1, plaid_account_id=$2, plaid_sync_from=$3
		 WHERE id=$4 AND user_id=$5`,
		itemID, plaidAccountID, syncFrom, accountID, userID)
	if err != nil {
		return fmt.Errorf("link account to plaid: %w", err)
	}
	return nil
}

// UnlinkAccountFromPlaid clears the account's bank link. Imported transactions
// stay — they are the user's ledger.
func (s *Store) UnlinkAccountFromPlaid(ctx context.Context, userID, accountID int64) error {
	_, err := s.run(ctx,
		`UPDATE accounts SET plaid_item_id=NULL, plaid_account_id=NULL, plaid_sync_from=NULL
		 WHERE id=$1 AND user_id=$2`, accountID, userID)
	return err
}

// CountAccountsForPlaidItem reports how many accounts still link to the item —
// zero means the item itself can be removed at Plaid and deleted.
func (s *Store) CountAccountsForPlaidItem(ctx context.Context, userID, itemID int64) (int, error) {
	var n int
	err := s.queryOne(ctx,
		`SELECT COUNT(*) FROM accounts WHERE plaid_item_id=$1 AND user_id=$2`, itemID, userID).Scan(&n)
	return n, err
}

// ListLinkedAccounts returns the item's linked accounts (plaid account id →
// app account), used by the sync engine to route imported transactions.
func (s *Store) ListLinkedAccounts(ctx context.Context, itemID int64) ([]Account, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, name, type, starting_balance_cents, credit_limit_cents, apr_bps,
		        monthly_payment_cents, include_in_paydown, payment_category_id,
		        plaid_item_id, plaid_account_id, plaid_sync_from,
		        archived_at, created_at
		 FROM accounts WHERE plaid_item_id=$1`, itemID)
	if err != nil {
		return nil, fmt.Errorf("list linked accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Account
	for rows.Next() {
		var a Account
		var typ string
		var lim, apr, pay, payCat, plaidItem sql.NullInt64
		var plaidAcct sql.NullString
		var archived, syncFrom nullTime
		if err := rows.Scan(&a.ID, &a.Name, &typ, &a.StartingBalanceCents, &lim, &apr,
			&pay, &a.IncludeInPaydown, &payCat, &plaidItem, &plaidAcct, &syncFrom,
			&archived, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Type = AccountType(typ)
		a.CreditLimitCents = intPtr(lim)
		a.AprBps = intPtr(apr)
		a.MonthlyPaymentCents = intPtr(pay)
		a.PaymentCategoryID = intPtr(payCat)
		a.PlaidItemID = intPtr(plaidItem)
		a.PlaidAccountID = strPtr(plaidAcct)
		a.PlaidSyncFrom = syncFrom.Ptr()
		a.ArchivedAt = archived.Ptr()
		out = append(out, a)
	}
	return out, rows.Err()
}
