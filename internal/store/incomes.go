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

func (s *Store) CreateIncome(ctx context.Context, in Income) (int64, error) {
	if in.Month == "" || in.Name == "" {
		return 0, errors.New("month and name required")
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO incomes(month, name, amount_cents, sort_order) VALUES (?, ?, ?, ?)`,
		in.Month, in.Name, in.AmountCents, in.SortOrder)
	if err != nil {
		return 0, fmt.Errorf("create income: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateIncome(ctx context.Context, in Income) error {
	_, err := s.run(ctx,
		`UPDATE incomes SET name=?, amount_cents=?, sort_order=? WHERE id=?`,
		in.Name, in.AmountCents, in.SortOrder, in.ID)
	return err
}

func (s *Store) DeleteIncome(ctx context.Context, id int64) error {
	_, err := s.run(ctx, `DELETE FROM incomes WHERE id=?`, id)
	return err
}

// ListIncomes returns all income rows for a month, ordered.
func (s *Store) ListIncomes(ctx context.Context, month string) ([]Income, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, month, name, amount_cents, sort_order
		 FROM incomes WHERE month=? ORDER BY sort_order, id`, month)
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
func (s *Store) TotalIncome(ctx context.Context, month string) (int64, error) {
	var total int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM incomes WHERE month=?`, month).Scan(&total)
	return total, err
}
