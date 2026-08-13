package plaid

import (
	"context"
	"fmt"
	"time"

	plaidapi "github.com/plaid/plaid-go/v45/plaid"

	"github.com/sbengtson/budget/internal/core/store"
)

// advisoryLockClass namespaces this app's per-item sync locks within
// Postgres's two-int advisory lock space.
const advisoryLockClass = 7159

// maxSyncRestarts bounds the retry loop when Plaid mutates data mid-pagination.
const maxSyncRestarts = 3

// SyncItem pulls every pending update for one item through
// /transactions/sync and applies it to the store. Safe to call from the
// webhook handler, the polling worker, and the link flow concurrently: a
// Postgres advisory lock serializes syncs per item, and a second caller
// simply returns (the holder will pick up its changes).
//
// The cursor advances only after a fully paginated batch applies atomically
// (store.ApplyPlaidSync), so an interruption at any point replays cleanly.
func (s *Service) SyncItem(ctx context.Context, item store.PlaidItem) error {
	if !s.configured {
		return ErrNotConfigured
	}
	conn, err := s.store.DB().Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var locked bool
	if err := conn.QueryRowContext(ctx,
		`SELECT pg_try_advisory_lock($1, $2)`, advisoryLockClass, item.ID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil // another sync of this item is in flight
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock($1, $2)`, advisoryLockClass, item.ID)
	}()

	token, err := s.sealer.Open(item.AccessTokenEncrypted)
	if err != nil {
		return fmt.Errorf("unseal access token for item %d: %w", item.ID, err)
	}

	accounts, err := s.store.ListLinkedAccounts(ctx, item.ID)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return nil // nothing mapped yet; the map step triggers a sync when done
	}
	byPlaidID := make(map[string]store.Account, len(accounts))
	for _, a := range accounts {
		if a.PlaidAccountID != nil {
			byPlaidID[*a.PlaidAccountID] = a
		}
	}

	// The initial backfill (no cursor yet) imports pre-reviewed: flooding a
	// fresh link with 90 days of "needs review" would bury the flag's signal.
	initial := item.SyncCursor == ""

	batch, err := s.pageSync(ctx, token, item, byPlaidID, initial)
	if err != nil {
		s.recordSyncError(ctx, item, err)
		return err
	}
	if err := s.store.ApplyPlaidSync(ctx, item.UserID, item.ID, *batch); err != nil {
		return err
	}
	if item.Status != store.PlaidItemActive {
		_ = s.store.SetPlaidItemStatus(ctx, item.ID, store.PlaidItemActive, "")
	}
	return nil
}

// pageSync walks /transactions/sync to has_more=false, restarting from the
// item's stored cursor when Plaid reports a mutation mid-pagination.
func (s *Service) pageSync(ctx context.Context, token string, item store.PlaidItem, byPlaidID map[string]store.Account, initial bool) (*store.PlaidSyncBatch, error) {
restart:
	for attempt := 0; attempt < maxSyncRestarts; attempt++ {
		batch := &store.PlaidSyncBatch{}
		cursor := item.SyncCursor
		for {
			req := plaidapi.NewTransactionsSyncRequest(token)
			if cursor != "" {
				req.SetCursor(cursor)
			}
			resp, err := s.api.TransactionsSync(ctx, *req)
			if err != nil {
				if errorCode(err) == "TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION" {
					logSyncWarn("mutation during pagination; restarting", item.ID, "")
					continue restart
				}
				return nil, err
			}
			for _, t := range resp.GetAdded() {
				if u, ok := s.mapTransaction(t, byPlaidID, initial); ok {
					batch.Upserts = append(batch.Upserts, u)
				}
			}
			for _, t := range resp.GetModified() {
				if u, ok := s.mapTransaction(t, byPlaidID, initial); ok {
					batch.Upserts = append(batch.Upserts, u)
				}
			}
			for _, r := range resp.GetRemoved() {
				batch.RemovedIDs = append(batch.RemovedIDs, r.GetTransactionId())
			}
			cursor = resp.GetNextCursor()
			if !resp.GetHasMore() {
				batch.NextCursor = cursor
				return batch, nil
			}
		}
	}
	return nil, fmt.Errorf("sync item %d: pagination kept mutating after %d restarts", item.ID, maxSyncRestarts)
}

// mapTransaction converts one Plaid transaction into a store upsert. Returns
// false for transactions the sync must skip: unmapped bank accounts, and
// anything dated before the account's import boundary (plaid_sync_from), which
// is what keeps a linked pre-existing account from double-counting history.
func (s *Service) mapTransaction(t plaidapi.Transaction, byPlaidID map[string]store.Account, initial bool) (store.PlaidTxUpsert, bool) {
	acct, ok := byPlaidID[t.GetAccountId()]
	if !ok {
		return store.PlaidTxUpsert{}, false
	}
	date, err := time.Parse("2006-01-02", t.GetDate())
	if err != nil {
		logSyncWarn("unparseable transaction date; skipping", acct.ID, "")
		return store.PlaidTxUpsert{}, false
	}
	if acct.PlaidSyncFrom != nil && date.Before(*acct.PlaidSyncFrom) {
		return store.PlaidTxUpsert{}, false
	}

	u := store.PlaidTxUpsert{
		PlaidTransactionID:   t.GetTransactionId(),
		PendingTransactionID: t.GetPendingTransactionId(),
		AccountID:            acct.ID,
		Date:                 date,
		Payee:                t.GetMerchantName(),
		Cleared:              !t.GetPending(),
		NeedsReview:          !initial,
	}
	if u.Payee == "" {
		u.Payee = t.GetName()
	}
	// Plaid's sign convention: positive = money out, negative = money in.
	if amount := t.GetAmount(); amount >= 0 {
		u.OutflowCents = toCents(amount)
	} else {
		u.InflowCents = toCents(-amount)
	}
	return u, true
}

// recordSyncError translates a failed sync into item status. ITEM_LOGIN_REQUIRED
// flips the item into the reconnect flow; anything else parks it in error with
// a token-free message.
func (s *Service) recordSyncError(ctx context.Context, item store.PlaidItem, err error) {
	code := errorCode(err)
	switch code {
	case "ITEM_LOGIN_REQUIRED":
		_ = s.store.SetPlaidItemStatus(ctx, item.ID, store.PlaidItemLoginRequired, code)
	default:
		if code == "" {
			code = "sync failed"
		}
		_ = s.store.SetPlaidItemStatus(ctx, item.ID, store.PlaidItemError, code)
	}
	logSyncWarn("sync failed", item.ID, code)
}
