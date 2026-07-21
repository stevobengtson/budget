package store

import (
	"context"
	"errors"
	"fmt"
)

type Income struct {
	ID          int64
	Month       string // YYYY-MM
	Name        string
	AmountCents int64
	SortOrder   int64
}

func (s *Store) CreateIncome(ctx context.Context, userID int64, in Income) (int64, error) {
	if in.Month == "" || in.Name == "" {
		return 0, errors.New("month and name required")
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO incomes(user_id, month, name, amount_cents, sort_order) VALUES ($1, $2, $3, $4, $5)`,
		userID, in.Month, in.Name, in.AmountCents, in.SortOrder)
	if err != nil {
		return 0, fmt.Errorf("create income: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateIncome(ctx context.Context, userID int64, in Income) error {
	_, err := s.run(ctx,
		`UPDATE incomes SET name=$1, amount_cents=$2, sort_order=$3 WHERE id=$4 AND user_id=$5`,
		in.Name, in.AmountCents, in.SortOrder, in.ID, userID)
	return err
}

func (s *Store) DeleteIncome(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx, `DELETE FROM incomes WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// ListIncomes returns all income rows for a month, ordered.
func (s *Store) ListIncomes(ctx context.Context, userID int64, month string) ([]Income, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, month, name, amount_cents, sort_order
		 FROM incomes WHERE month=$1 AND user_id=$2 ORDER BY sort_order, id`, month, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Income
	for rows.Next() {
		var i Income
		if err := rows.Scan(&i.ID, &i.Month, &i.Name, &i.AmountCents, &i.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// TotalIncome sums amount_cents for the month.
func (s *Store) TotalIncome(ctx context.Context, userID int64, month string) (int64, error) {
	var total int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM incomes WHERE month=$1 AND user_id=$2`, month, userID).Scan(&total)
	return total, err
}

// MaxIncomeSortOrder returns the largest sort_order among income rows in a
// month, or 0 if there are none. Callers add 1 to append.
func (s *Store) MaxIncomeSortOrder(ctx context.Context, userID int64, month string) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM incomes WHERE month=$1 AND user_id=$2`, month, userID).Scan(&max)
	return max, err
}
