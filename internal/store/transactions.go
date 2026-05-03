package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	CreatedAt         time.Time
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

func (s *Store) CreateTransaction(ctx context.Context, t Transaction) (int64, error) {
	if t.OutflowCents < 0 || t.InflowCents < 0 {
		return 0, errors.New("outflow/inflow must be non-negative")
	}
	if t.OutflowCents > 0 && t.InflowCents > 0 {
		return 0, errors.New("transaction has both outflow and inflow")
	}
	if t.TransferAccountID != nil {
		return 0, errors.New("use CreateTransfer for transfers")
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO transactions(date, account_id, category_id, payee, notes, outflow_cents, inflow_cents, cleared)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Date.Format("2006-01-02"), t.AccountID, nullInt(t.CategoryID),
		nullStr(t.Payee), nullStr(t.Notes), t.OutflowCents, t.InflowCents, t.Cleared)
	if err != nil {
		return 0, fmt.Errorf("create transaction: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateTransaction(ctx context.Context, t Transaction) error {
	if t.TransferPairID != nil {
		return errors.New("update transfers via DeleteTransaction + CreateTransfer")
	}
	_, err := s.run(ctx,
		`UPDATE transactions
		 SET date=?, account_id=?, category_id=?, payee=?, notes=?, outflow_cents=?, inflow_cents=?, cleared=?
		 WHERE id=?`,
		t.Date.Format("2006-01-02"), t.AccountID, nullInt(t.CategoryID),
		nullStr(t.Payee), nullStr(t.Notes), t.OutflowCents, t.InflowCents, t.Cleared, t.ID)
	return err
}

// DeleteTransaction removes a transaction. If it's part of a transfer, both
// legs are removed atomically.
func (s *Store) DeleteTransaction(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var pair sql.NullInt64
	if err := s.txQueryOne(ctx, tx, `SELECT transfer_pair_id FROM transactions WHERE id=?`, id).Scan(&pair); err != nil {
		return err
	}
	if pair.Valid {
		// Break the cycle so neither row references the other before deletion.
		if _, err := s.txExec(ctx, tx, `UPDATE transactions SET transfer_pair_id=NULL WHERE id IN (?, ?)`, id, pair.Int64); err != nil {
			return err
		}
		if _, err := s.txExec(ctx, tx, `DELETE FROM transactions WHERE id=?`, pair.Int64); err != nil {
			return err
		}
	}
	if _, err := s.txExec(ctx, tx, `DELETE FROM transactions WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateTransfer inserts two linked rows in a single SQL transaction.
// Returns IDs (fromLegID, toLegID).
func (s *Store) CreateTransfer(ctx context.Context, in TransferInput) (int64, int64, error) {
	if in.AmountCents <= 0 {
		return 0, 0, errors.New("transfer amount must be positive")
	}
	if in.FromAccountID == in.ToAccountID {
		return 0, 0, errors.New("from and to accounts must differ")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	dateStr := in.Date.Format("2006-01-02")

	outID, err := s.txInsertReturningID(ctx, tx,
		`INSERT INTO transactions(date, account_id, transfer_account_id, category_id, notes, outflow_cents, cleared)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		dateStr, in.FromAccountID, in.ToAccountID, nullInt(in.CategoryID), nullStr(in.Notes), in.AmountCents, in.Cleared)
	if err != nil {
		return 0, 0, fmt.Errorf("transfer out leg: %w", err)
	}

	inID, err := s.txInsertReturningID(ctx, tx,
		`INSERT INTO transactions(date, account_id, transfer_account_id, transfer_pair_id, notes, inflow_cents, cleared)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		dateStr, in.ToAccountID, in.FromAccountID, outID, nullStr(in.Notes), in.AmountCents, in.Cleared)
	if err != nil {
		return 0, 0, fmt.Errorf("transfer in leg: %w", err)
	}

	if _, err := s.txExec(ctx, tx,
		`UPDATE transactions SET transfer_pair_id=? WHERE id=?`, inID, outID); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return outID, inID, nil
}

type TxFilter struct {
	AccountID  *int64
	CategoryID *int64
	Month      string // "YYYY-MM"; empty = no month filter
	Limit      int
}

func (s *Store) ListTransactions(ctx context.Context, f TxFilter) ([]Transaction, error) {
	q := `SELECT id, date, account_id, category_id, transfer_account_id, transfer_pair_id,
	             payee, notes, outflow_cents, inflow_cents, cleared, created_at
	      FROM transactions WHERE 1=1`
	args := []any{}
	if f.AccountID != nil {
		q += ` AND account_id=?`
		args = append(args, *f.AccountID)
	}
	if f.CategoryID != nil {
		q += ` AND category_id=?`
		args = append(args, *f.CategoryID)
	}
	if f.Month != "" {
		q += ` AND ` + s.dialect.MonthExpr("date") + ` = ?`
		args = append(args, f.Month)
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
		var payee, notes sql.NullString
		if err := rows.Scan(&t.ID, &t.Date, &t.AccountID, &cat, &transferAcc, &pair,
			&payee, &notes, &t.OutflowCents, &t.InflowCents, &t.Cleared, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.CategoryID = intPtr(cat)
		t.TransferAccountID = intPtr(transferAcc)
		t.TransferPairID = intPtr(pair)
		t.Payee = strPtr(payee)
		t.Notes = strPtr(notes)
		out = append(out, t)
	}
	return out, rows.Err()
}
