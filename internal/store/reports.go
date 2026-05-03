package store

import (
	"context"
	"fmt"
	"time"
)

// CategorySpend is a row in the SpendingByCategory report.
type CategorySpend struct {
	CategoryID   int64
	CategoryName string
	GroupName    string
	OutflowCents int64
}

// SpendingByCategory totals outflows grouped by category between since and
// until (both inclusive on the date column). Excludes transfers (rows where
// transfer_account_id is set). Sorted by outflow descending.
func (s *Store) SpendingByCategory(ctx context.Context, since, until time.Time) ([]CategorySpend, error) {
	rows, err := s.queryAll(ctx, `
SELECT c.id, c.name, g.name, COALESCE(SUM(t.outflow_cents), 0) AS spent
FROM categories c
JOIN category_groups g ON g.id = c.group_id
LEFT JOIN transactions t
       ON t.category_id = c.id
      AND t.transfer_account_id IS NULL
      AND t.date BETWEEN ? AND ?
GROUP BY c.id
HAVING spent > 0
ORDER BY spent DESC`,
		since.Format("2006-01-02"), until.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("spending by category: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CategorySpend
	for rows.Next() {
		var r CategorySpend
		if err := rows.Scan(&r.CategoryID, &r.CategoryName, &r.GroupName, &r.OutflowCents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MonthCashflow is one month of inflow / outflow totals plus configured
// income for that month.
type MonthCashflow struct {
	Month         string // YYYY-MM
	IncomeCents   int64  // sum of incomes table for that month
	InflowCents   int64  // actual inflow transactions (excl. transfers)
	OutflowCents  int64  // actual outflow transactions (excl. transfers)
}

// MonthlyCashflow returns the last `months` months including the current
// month, oldest first.
func (s *Store) MonthlyCashflow(ctx context.Context, months int) ([]MonthCashflow, error) {
	if months <= 0 {
		months = 12
	}
	now := time.Now()
	out := make([]MonthCashflow, 0, months)
	for i := months - 1; i >= 0; i-- {
		m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -i, 0)
		key := m.Format("2006-01")
		row := MonthCashflow{Month: key}

		if err := s.queryOne(ctx,
			`SELECT COALESCE(SUM(amount_cents), 0) FROM incomes WHERE month = ?`,
			key).Scan(&row.IncomeCents); err != nil {
			return nil, err
		}
		q := fmt.Sprintf(
			`SELECT COALESCE(SUM(inflow_cents), 0), COALESCE(SUM(outflow_cents), 0)
			 FROM transactions
			 WHERE %s = ?
			   AND transfer_account_id IS NULL`, s.dialect.MonthExpr("date"))
		if err := s.queryOne(ctx, q, key).Scan(&row.InflowCents, &row.OutflowCents); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}
