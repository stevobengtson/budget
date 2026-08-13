package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Transaction struct {
	ID                int64
	Date              time.Time
	AccountID         int64
	CategoryID        *int64
	TransferAccountID *int64
	TransferPairID    *int64
	Payee             *string
	Notes             *string
	OutflowCents      int64
	InflowCents       int64
	Cleared           bool
	// PlaidTransactionID marks a row imported by bank sync (nil = manual entry);
	// NeedsReview flags imports the user hasn't looked at yet. Both are written
	// by ApplyPlaidSync, not by the manual create/update paths.
	PlaidTransactionID *string
	NeedsReview        bool
	CreatedAt          time.Time
}

// TransferInput describes a transfer between two accounts.
//
// CategoryID, if set, attaches to the **from-leg** only — the side that
// represents the spending event. The to-leg stays uncategorized so the
// inflow doesn't double-count in budget reports. This is the standard
// envelope-budgeting pattern for paying credit cards / lines of credit:
// the payment shows up as spent against a "CC Payment" category while
// still moving money between accounts.
type TransferInput struct {
	Date          time.Time
	FromAccountID int64
	ToAccountID   int64
	AmountCents   int64 // positive
	CategoryID    *int64
	Notes         *string
	Cleared       bool
}

func (s *Store) CreateTransaction(ctx context.Context, userID int64, t Transaction) (int64, error) {
	if t.OutflowCents < 0 || t.InflowCents < 0 {
		return 0, errors.New("outflow/inflow must be non-negative")
	}
	if t.OutflowCents > 0 && t.InflowCents > 0 {
		return 0, errors.New("transaction has both outflow and inflow")
	}
	if t.TransferAccountID != nil {
		return 0, errors.New("use CreateTransfer for transfers")
	}
	if err := s.requireAccount(ctx, userID, t.AccountID); err != nil {
		return 0, err
	}
	if t.CategoryID != nil {
		if err := s.requireCategory(ctx, userID, *t.CategoryID); err != nil {
			return 0, err
		}
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO transactions(user_id, date, account_id, category_id, payee, notes, outflow_cents, inflow_cents, cleared)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		userID, t.Date.Format("2006-01-02"), t.AccountID, nullInt(t.CategoryID),
		nullStr(t.Payee), nullStr(t.Notes), t.OutflowCents, t.InflowCents, t.Cleared)
	if err != nil {
		return 0, fmt.Errorf("create transaction: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateTransaction(ctx context.Context, userID int64, t Transaction) error {
	if t.TransferPairID != nil {
		return errors.New("update transfers via DeleteTransaction + CreateTransfer")
	}
	if err := s.requireAccount(ctx, userID, t.AccountID); err != nil {
		return err
	}
	if t.TransferAccountID != nil {
		if err := s.requireAccount(ctx, userID, *t.TransferAccountID); err != nil {
			return err
		}
	}
	if t.CategoryID != nil {
		if err := s.requireCategory(ctx, userID, *t.CategoryID); err != nil {
			return err
		}
	}
	// Assigning a category to a needs-review import IS the review, so the flag
	// clears itself; an update that leaves the row uncategorized keeps it.
	_, err := s.run(ctx,
		`UPDATE transactions
		 SET date=$1, account_id=$2, category_id=$3, payee=$4, notes=$5, outflow_cents=$6, inflow_cents=$7, cleared=$8,
		     needs_review = CASE WHEN $3::bigint IS NOT NULL THEN FALSE ELSE needs_review END
		 WHERE id=$9 AND user_id=$10`,
		t.Date.Format("2006-01-02"), t.AccountID, nullInt(t.CategoryID),
		nullStr(t.Payee), nullStr(t.Notes), t.OutflowCents, t.InflowCents, t.Cleared, t.ID, userID)
	return err
}

// SetTransactionReviewed clears one transaction's needs_review flag (the row's
// "mark reviewed" action).
func (s *Store) SetTransactionReviewed(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx,
		`UPDATE transactions SET needs_review=FALSE WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// MarkAllReviewed clears needs_review on every transaction matching the
// filter's account/category/month scope, returning how many rows changed.
func (s *Store) MarkAllReviewed(ctx context.Context, userID int64, f TxFilter) (int64, error) {
	q := `UPDATE transactions SET needs_review=FALSE WHERE user_id=$1 AND needs_review`
	args := []any{userID}
	if f.AccountID != nil {
		args = append(args, *f.AccountID)
		q += fmt.Sprintf(` AND account_id=$%d`, len(args))
	}
	if f.CategoryID != nil {
		args = append(args, *f.CategoryID)
		q += fmt.Sprintf(` AND category_id=$%d`, len(args))
	}
	if f.Month != "" {
		args = append(args, f.Month)
		q += fmt.Sprintf(` AND %s = $%d`, monthExpr("date"), len(args))
	}
	res, err := s.run(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CountNeedsReview is the user's total unreviewed-import count, shown as the
// nav badge. Cheap: served by the partial index on (user_id) WHERE needs_review.
func (s *Store) CountNeedsReview(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.queryOne(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id=$1 AND needs_review`, userID).Scan(&n)
	return n, err
}

// GetTransaction fetches a single transaction by id.
func (s *Store) GetTransaction(ctx context.Context, userID, id int64) (*Transaction, error) {
	q := `SELECT id, date, account_id, category_id, transfer_account_id, transfer_pair_id,
	             payee, notes, outflow_cents, inflow_cents, cleared,
	             plaid_transaction_id, needs_review, created_at
	      FROM transactions WHERE id=$1 AND user_id=$2`
	var t Transaction
	var cat, transferAcc, pair sql.NullInt64
	var payee, notes, plaidID sql.NullString
	err := s.queryOne(ctx, q, id, userID).Scan(&t.ID, &t.Date, &t.AccountID, &cat, &transferAcc, &pair,
		&payee, &notes, &t.OutflowCents, &t.InflowCents, &t.Cleared,
		&plaidID, &t.NeedsReview, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	t.CategoryID = intPtr(cat)
	t.TransferAccountID = intPtr(transferAcc)
	t.TransferPairID = intPtr(pair)
	t.Payee = strPtr(payee)
	t.Notes = strPtr(notes)
	t.PlaidTransactionID = strPtr(plaidID)
	return &t, nil
}

// TransferLegEdit describes the desired new state of the ONE leg the user
// edited. UpdateTransfer keeps the paired leg consistent:
//   - the amount mirrors with the opposite direction (edit outflow -> pair inflow)
//   - date and cleared always match on both legs
//   - the paired leg's account follows this leg's "transfer to" selection
//   - the paired leg's transfer_account back-points at this leg's account, so
//     the "from/to" label stays truthful
//   - the category always lands on the outflow (spending) leg per envelope rules;
//     the inflow leg is left uncategorized
type TransferLegEdit struct {
	Date              time.Time
	AccountID         int64 // this leg's account
	TransferAccountID int64 // this leg's counter-account (where the pair posts)
	OutflowCents      int64 // exactly one of Outflow/Inflow must be > 0
	InflowCents       int64
	CategoryID        *int64
	Notes             *string
}

// UpdateTransfer updates both legs of the transfer that legID belongs to,
// atomically, from the edited leg's new state. See TransferLegEdit.
func (s *Store) UpdateTransfer(ctx context.Context, userID, legID int64, in TransferLegEdit) error {
	if in.OutflowCents < 0 || in.InflowCents < 0 {
		return errors.New("outflow/inflow must be non-negative")
	}
	if (in.OutflowCents > 0) == (in.InflowCents > 0) {
		return errors.New("transfer leg must have exactly one of outflow/inflow")
	}
	if in.AccountID == in.TransferAccountID {
		return errors.New("from and to accounts must differ")
	}
	if err := s.requireAccount(ctx, userID, in.AccountID); err != nil {
		return err
	}
	if err := s.requireAccount(ctx, userID, in.TransferAccountID); err != nil {
		return err
	}
	if in.CategoryID != nil {
		if err := s.requireCategory(ctx, userID, *in.CategoryID); err != nil {
			return err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var pair sql.NullInt64
	if err := s.txQueryOne(ctx, tx, `SELECT transfer_pair_id FROM transactions WHERE id=$1 AND user_id=$2`, legID, userID).Scan(&pair); err != nil {
		return err
	}
	if !pair.Valid {
		return errors.New("transaction is not part of a transfer")
	}
	pairID := pair.Int64

	dateStr := in.Date.Format("2006-01-02")
	// The category belongs on whichever leg carries the outflow (the spending
	// side). When the edited leg is the outflow, it keeps the category and the
	// pair is cleared; otherwise the pair becomes the outflow and holds it.
	editedIsOutflow := in.OutflowCents > 0
	var editedCat, pairCat *int64
	if editedIsOutflow {
		editedCat = in.CategoryID
	} else {
		pairCat = in.CategoryID
	}

	// Edited leg: exactly the values the user chose. cleared is left out of both
	// statements on purpose — TransferLegEdit carries no such field because
	// reconciling is SetCleared's job, and writing the column here would
	// un-reconcile a matched transfer on any unrelated edit.
	if _, err := s.txExec(ctx, tx,
		`UPDATE transactions
		 SET date=$1, account_id=$2, transfer_account_id=$3, category_id=$4, notes=$5,
		     outflow_cents=$6, inflow_cents=$7
		 WHERE id=$8 AND user_id=$9`,
		dateStr, in.AccountID, in.TransferAccountID, nullInt(editedCat), nullStr(in.Notes),
		in.OutflowCents, in.InflowCents, legID, userID); err != nil {
		return fmt.Errorf("update edited leg: %w", err)
	}

	// Paired leg: mirror amount/direction; account follows "transfer to";
	// back-pointer follows the edited leg's account.
	if _, err := s.txExec(ctx, tx,
		`UPDATE transactions
		 SET date=$1, account_id=$2, transfer_account_id=$3, category_id=$4, notes=$5,
		     outflow_cents=$6, inflow_cents=$7
		 WHERE id=$8 AND user_id=$9`,
		dateStr, in.TransferAccountID, in.AccountID, nullInt(pairCat), nullStr(in.Notes),
		in.InflowCents, in.OutflowCents, pairID, userID); err != nil {
		return fmt.Errorf("update paired leg: %w", err)
	}

	return tx.Commit()
}

// SetCleared sets the cleared flag on a transaction. If it's part of a
// transfer, both legs are updated so their cleared state stays identical.
func (s *Store) SetCleared(ctx context.Context, userID, id int64, cleared bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var pair sql.NullInt64
	if err := s.txQueryOne(ctx, tx, `SELECT transfer_pair_id FROM transactions WHERE id=$1 AND user_id=$2`, id, userID).Scan(&pair); err != nil {
		return err
	}
	// Build the placeholder list sequentially: $1 = cleared, then one $n per id,
	// then user_id last.
	args := []any{cleared}
	ids := []int64{id}
	if pair.Valid {
		ids = append(ids, pair.Int64)
	}
	inClauses := make([]string, len(ids))
	for i, tid := range ids {
		args = append(args, tid)
		inClauses[i] = fmt.Sprintf("$%d", len(args))
	}
	args = append(args, userID)
	q := fmt.Sprintf(`UPDATE transactions SET cleared=$1 WHERE id IN (%s) AND user_id=$%d`,
		strings.Join(inClauses, ", "), len(args))
	if _, err := s.txExec(ctx, tx, q, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTransaction removes a transaction. If it's part of a transfer, both
// legs are removed atomically.
func (s *Store) DeleteTransaction(ctx context.Context, userID, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var pair sql.NullInt64
	if err := s.txQueryOne(ctx, tx, `SELECT transfer_pair_id FROM transactions WHERE id=$1 AND user_id=$2`, id, userID).Scan(&pair); err != nil {
		return err
	}
	if pair.Valid {
		// Break the cycle so neither row references the other before deletion.
		if _, err := s.txExec(ctx, tx, `UPDATE transactions SET transfer_pair_id=NULL WHERE id IN ($1, $2) AND user_id=$3`, id, pair.Int64, userID); err != nil {
			return err
		}
		if _, err := s.txExec(ctx, tx, `DELETE FROM transactions WHERE id=$1 AND user_id=$2`, pair.Int64, userID); err != nil {
			return err
		}
	}
	if _, err := s.txExec(ctx, tx, `DELETE FROM transactions WHERE id=$1 AND user_id=$2`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateTransfer inserts two linked rows in a single SQL transaction.
// Returns IDs (fromLegID, toLegID).
func (s *Store) CreateTransfer(ctx context.Context, userID int64, in TransferInput) (int64, int64, error) {
	if in.AmountCents <= 0 {
		return 0, 0, errors.New("transfer amount must be positive")
	}
	if in.FromAccountID == in.ToAccountID {
		return 0, 0, errors.New("from and to accounts must differ")
	}
	if err := s.requireAccount(ctx, userID, in.FromAccountID); err != nil {
		return 0, 0, err
	}
	if err := s.requireAccount(ctx, userID, in.ToAccountID); err != nil {
		return 0, 0, err
	}
	if in.CategoryID != nil {
		if err := s.requireCategory(ctx, userID, *in.CategoryID); err != nil {
			return 0, 0, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	dateStr := in.Date.Format("2006-01-02")

	outID, err := s.txInsertReturningID(ctx, tx,
		`INSERT INTO transactions(user_id, date, account_id, transfer_account_id, category_id, notes, outflow_cents, cleared)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID, dateStr, in.FromAccountID, in.ToAccountID, nullInt(in.CategoryID), nullStr(in.Notes), in.AmountCents, in.Cleared)
	if err != nil {
		return 0, 0, fmt.Errorf("transfer out leg: %w", err)
	}

	inID, err := s.txInsertReturningID(ctx, tx,
		`INSERT INTO transactions(user_id, date, account_id, transfer_account_id, transfer_pair_id, notes, inflow_cents, cleared)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID, dateStr, in.ToAccountID, in.FromAccountID, outID, nullStr(in.Notes), in.AmountCents, in.Cleared)
	if err != nil {
		return 0, 0, fmt.Errorf("transfer in leg: %w", err)
	}

	if _, err := s.txExec(ctx, tx,
		`UPDATE transactions SET transfer_pair_id=$1 WHERE id=$2 AND user_id=$3`, inID, outID, userID); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return outID, inID, nil
}

type TxFilter struct {
	AccountID   *int64
	CategoryID  *int64
	Month       string // "YYYY-MM"; empty = no month filter
	NeedsReview bool   // true = only unreviewed bank-sync imports
	Limit       int
}

func (s *Store) ListTransactions(ctx context.Context, userID int64, f TxFilter) ([]Transaction, error) {
	q := `SELECT id, date, account_id, category_id, transfer_account_id, transfer_pair_id,
	             payee, notes, outflow_cents, inflow_cents, cleared,
	             plaid_transaction_id, needs_review, created_at
	      FROM transactions WHERE user_id=$1`
	args := []any{userID}
	if f.AccountID != nil {
		args = append(args, *f.AccountID)
		q += fmt.Sprintf(` AND account_id=$%d`, len(args))
	}
	if f.CategoryID != nil {
		args = append(args, *f.CategoryID)
		q += fmt.Sprintf(` AND category_id=$%d`, len(args))
	}
	if f.Month != "" {
		args = append(args, f.Month)
		q += fmt.Sprintf(` AND %s = $%d`, monthExpr("date"), len(args))
	}
	if f.NeedsReview {
		q += ` AND needs_review`
	}
	q += ` ORDER BY date DESC, id DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}

	rows, err := s.queryAll(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Transaction
	for rows.Next() {
		var t Transaction
		var cat, transferAcc, pair sql.NullInt64
		var payee, notes, plaidID sql.NullString
		if err := rows.Scan(&t.ID, &t.Date, &t.AccountID, &cat, &transferAcc, &pair,
			&payee, &notes, &t.OutflowCents, &t.InflowCents, &t.Cleared,
			&plaidID, &t.NeedsReview, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.CategoryID = intPtr(cat)
		t.TransferAccountID = intPtr(transferAcc)
		t.TransferPairID = intPtr(pair)
		t.Payee = strPtr(payee)
		t.Notes = strPtr(notes)
		t.PlaidTransactionID = strPtr(plaidID)
		out = append(out, t)
	}
	return out, rows.Err()
}
