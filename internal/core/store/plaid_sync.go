package store

import (
	"context"
	"fmt"
	"time"
)

// PlaidTxUpsert is one added/modified transaction from /transactions/sync,
// already mapped to an app account and integer cents.
type PlaidTxUpsert struct {
	PlaidTransactionID   string
	PendingTransactionID string // the pending row this posted tx replaces; "" when none
	AccountID            int64
	Date                 time.Time
	Payee                string
	OutflowCents         int64
	InflowCents          int64
	Cleared              bool // posted; false = still pending
	NeedsReview          bool // false during the initial backfill
}

// PlaidSyncBatch is one fully paginated /transactions/sync result, applied
// atomically together with the cursor that produced it.
type PlaidSyncBatch struct {
	Upserts    []PlaidTxUpsert
	RemovedIDs []string
	NextCursor string
}

// ApplyPlaidSync applies a completed sync pass for one item in a single
// database transaction: upserts, removals, then the item's cursor +
// last_synced_at. All-or-nothing — an interrupted apply leaves the cursor
// untouched so the next sync replays the same window, and the unique
// plaid_transaction_id index makes that replay converge instead of duplicate.
//
// Upsert resolution order, per transaction:
//  1. a row already tracking this plaid_transaction_id → refresh it;
//  2. else a row tracking pending_transaction_id → the pending→posted
//     transition: re-point that row to the new id and refresh it;
//  3. else insert (ON CONFLICT DO NOTHING for webhook-burst replays).
//
// Refreshes preserve category_id and notes (the user's work) and re-flag
// needs_review only when the amount or date actually changed.
func (s *Store) ApplyPlaidSync(ctx context.Context, userID, itemID int64, batch PlaidSyncBatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const refresh = `
		UPDATE transactions SET
		   plaid_transaction_id = $1,
		   date = $2, payee = $3, outflow_cents = $4, inflow_cents = $5, cleared = $6,
		   needs_review = CASE
		     WHEN date <> $2 OR outflow_cents <> $4 OR inflow_cents <> $5 THEN TRUE
		     ELSE needs_review END
		 WHERE plaid_transaction_id = $7 AND user_id = $8`

	for _, u := range batch.Upserts {
		// 1. Refresh a row already tracking this id.
		res, err := s.txExec(ctx, tx, refresh,
			u.PlaidTransactionID, u.Date, u.Payee, u.OutflowCents, u.InflowCents, u.Cleared,
			u.PlaidTransactionID, userID)
		if err != nil {
			return fmt.Errorf("plaid sync refresh %s: %w", u.PlaidTransactionID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			continue
		}
		// 2. Pending → posted: re-point the pending row.
		if u.PendingTransactionID != "" {
			res, err = s.txExec(ctx, tx, refresh,
				u.PlaidTransactionID, u.Date, u.Payee, u.OutflowCents, u.InflowCents, u.Cleared,
				u.PendingTransactionID, userID)
			if err != nil {
				return fmt.Errorf("plaid sync repoint %s: %w", u.PlaidTransactionID, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				continue
			}
		}
		// 3. New row, uncategorized until the user reviews it.
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO transactions
			   (user_id, account_id, date, payee, outflow_cents, inflow_cents, cleared,
			    plaid_transaction_id, needs_review)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 ON CONFLICT (plaid_transaction_id) WHERE plaid_transaction_id IS NOT NULL DO NOTHING`,
			userID, u.AccountID, u.Date, u.Payee, u.OutflowCents, u.InflowCents, u.Cleared,
			u.PlaidTransactionID, u.NeedsReview); err != nil {
			return fmt.Errorf("plaid sync insert %s: %w", u.PlaidTransactionID, err)
		}
	}

	for _, id := range batch.RemovedIDs {
		if _, err := s.txExec(ctx, tx,
			`DELETE FROM transactions WHERE plaid_transaction_id = $1 AND user_id = $2`,
			id, userID); err != nil {
			return fmt.Errorf("plaid sync remove %s: %w", id, err)
		}
	}

	if _, err := s.txExec(ctx, tx,
		`UPDATE plaid_items SET sync_cursor=$1, last_synced_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		 WHERE id=$2`, batch.NextCursor, itemID); err != nil {
		return fmt.Errorf("plaid sync cursor: %w", err)
	}
	return tx.Commit()
}
