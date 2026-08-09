package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MonthKey returns "YYYY-MM" for t.
func MonthKey(t time.Time) string { return t.Format("2006-01") }

// PrevMonth returns "YYYY-MM" before m. m must be valid.
func PrevMonth(m string) string {
	t, _ := time.Parse("2006-01", m)
	return t.AddDate(0, -1, 0).Format("2006-01")
}

// SetAssigned upserts the assigned amount for (month, category).
func (s *Store) SetAssigned(ctx context.Context, userID int64, month string, categoryID, cents int64) error {
	// The ON CONFLICT target is (month, category_id) only, so without this guard
	// a caller could overwrite another user's assignment by passing that user's
	// category id. Reject any category the acting user does not own.
	if err := s.requireCategory(ctx, userID, categoryID); err != nil {
		return err
	}
	_, err := s.run(ctx,
		`INSERT INTO budgets(user_id, month, category_id, assigned_cents) VALUES ($1, $2, $3, $4)
		 ON CONFLICT(month, category_id) DO UPDATE SET assigned_cents=excluded.assigned_cents`,
		userID, month, categoryID, cents)
	return err
}

func (s *Store) GetAssigned(ctx context.Context, userID int64, month string, categoryID int64) (int64, error) {
	var c int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(assigned_cents, 0) FROM budgets WHERE month=$1 AND category_id=$2 AND user_id=$3`,
		month, categoryID, userID).Scan(&c)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return c, nil
}

// CategoryBudget summarises one category for a month.
type CategoryBudget struct {
	CategoryID     int64
	GroupID        int64
	GroupName      string
	CategoryName   string
	IsIncome       bool
	RolloverMode   string
	GoalCents      *int64
	GoalDueDate    *time.Time
	AssignedCents  int64
	SpentCents     int64
	AvailableCents int64
	MonthlyTarget  int64 // suggested assignment for sinking-fund goals
}

// MonthBudget computes assigned/spent/available for every active category in the month.
// Available = (carryover from prior months, ≥0) + assigned − spent.
func (s *Store) MonthBudget(ctx context.Context, userID int64, month string) ([]CategoryBudget, error) {
	q := `
SELECT c.id, c.group_id, g.name, c.name, c.is_income, c.rollover_mode, c.goal_cents, c.goal_due_date,
       COALESCE(b.assigned_cents, 0)                                              AS assigned,
       COALESCE((SELECT SUM(t.outflow_cents) - SUM(t.inflow_cents) FROM transactions t
                 WHERE t.category_id = c.id AND to_char(t.date, 'YYYY-MM') = $1 AND t.user_id = $2), 0) AS spent
FROM categories c
JOIN category_groups g ON g.id = c.group_id AND g.user_id = c.user_id
LEFT JOIN budgets b ON b.category_id = c.id AND b.month = $3 AND b.user_id = $4
WHERE c.archived_at IS NULL AND c.user_id = $5
ORDER BY g.sort_order, g.name, c.sort_order, c.name`
	rows, err := s.queryAll(ctx, q, month, userID, month, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("month budget: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CategoryBudget
	for rows.Next() {
		var cb CategoryBudget
		var goalCents nullableInt64
		var due nullTime
		if err := rows.Scan(&cb.CategoryID, &cb.GroupID, &cb.GroupName, &cb.CategoryName,
			&cb.IsIncome, &cb.RolloverMode, &goalCents, &due, &cb.AssignedCents, &cb.SpentCents); err != nil {
			return nil, err
		}
		if goalCents.Valid {
			v := goalCents.Int64
			cb.GoalCents = &v
		}
		cb.GoalDueDate = due.Ptr()
		out = append(out, cb)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Compute carryover & available per category, walking from earliest month
	// with data through the requested month. To keep things simple and correct,
	// we compute available as: lifetime_assigned_through_month + lifetime_inflow
	// − lifetime_spent_through_month, clipped at zero between months for the
	// previous-month carry rule.
	for i := range out {
		avail, err := s.categoryAvailable(ctx, userID, out[i].CategoryID, out[i].RolloverMode, month)
		if err != nil {
			return nil, err
		}
		out[i].AvailableCents = avail
		if out[i].GoalCents != nil && out[i].GoalDueDate != nil {
			out[i].MonthlyTarget = monthlyTarget(*out[i].GoalCents, avail, *out[i].GoalDueDate, month)
		}
	}
	return out, nil
}

// categoryAvailable walks every prior month and applies the category's rollover
// mode when carrying a running balance forward.
func (s *Store) categoryAvailable(ctx context.Context, userID, categoryID int64, mode string, month string) (int64, error) {
	// Find the earliest month that has either an assignment or a transaction.
	var earliest string
	q := `
SELECT MIN(m) FROM (
  SELECT month AS m FROM budgets WHERE category_id=$1 AND user_id=$2
  UNION
  SELECT to_char(date, 'YYYY-MM') AS m FROM transactions WHERE category_id=$3 AND user_id=$4
) AS sub`
	err := s.queryOne(ctx, q, categoryID, userID, categoryID, userID).Scan(&earliest)
	if err != nil || earliest == "" {
		// No data: available is just this month's assigned − spent (likely both 0).
		return monthAssignedMinusSpent(ctx, s, userID, categoryID, month)
	}

	carry := int64(0)
	cur := earliest
	for {
		delta, err := monthAssignedMinusSpent(ctx, s, userID, categoryID, cur)
		if err != nil {
			return 0, err
		}
		inbound := carry
		if mode == RolloverNone {
			inbound = 0 // isolated month: ignore any carried balance
		}
		avail := inbound + delta
		if cur == month {
			return avail, nil
		}
		switch mode {
		case RolloverNone:
			carry = 0
		case RolloverCarry:
			carry = avail
		default: // RolloverCarryPositive
			if avail < 0 {
				carry = 0
			} else {
				carry = avail
			}
		}
		next := nextMonth(cur)
		if next > month {
			// Gap: continue carrying through skipped months (no delta).
			return carry, nil
		}
		cur = next
	}
}

func monthAssignedMinusSpent(ctx context.Context, s *Store, userID, categoryID int64, month string) (int64, error) {
	var assigned, spent int64
	if err := s.queryOne(ctx,
		`SELECT COALESCE((SELECT assigned_cents FROM budgets WHERE month=$1 AND category_id=$2 AND user_id=$3), 0)`,
		month, categoryID, userID).Scan(&assigned); err != nil {
		return 0, err
	}
	q := `SELECT COALESCE(SUM(outflow_cents) - SUM(inflow_cents), 0)
		 FROM transactions WHERE category_id=$1 AND to_char(date, 'YYYY-MM')=$2 AND user_id=$3`
	if err := s.queryOne(ctx, q, categoryID, month, userID).Scan(&spent); err != nil {
		return 0, err
	}
	return assigned - spent, nil
}

func nextMonth(m string) string {
	t, _ := time.Parse("2006-01", m)
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// monthlyTarget = max(0, (goal − available) / months_left), where months_left
// is at least 1. Calculated from the first day of the requested month.
// MonthlyTarget is what has to go into a category each month to reach its goal
// by its due date, given what is already in it. Exported so the goal editor can
// preview the figure while it is being typed, without a second implementation
// of the arithmetic drifting from this one.
func MonthlyTarget(goalCents, availableCents int64, due time.Time, month string) int64 {
	return monthlyTarget(goalCents, availableCents, due, month)
}

func monthlyTarget(goalCents, availableCents int64, due time.Time, month string) int64 {
	monthStart, _ := time.Parse("2006-01", month)
	monthsLeft := monthsBetween(monthStart, due)
	if monthsLeft < 1 {
		monthsLeft = 1
	}
	remaining := goalCents - availableCents
	if remaining <= 0 {
		return 0
	}
	return remaining / int64(monthsLeft)
}

func monthsBetween(from, to time.Time) int {
	years := to.Year() - from.Year()
	months := int(to.Month()) - int(from.Month())
	total := years*12 + months
	if to.Day() > from.Day() {
		total++ // round up if due date is later in the month than month start
	}
	return total
}

// CreditActivity describes one credit account's activity for a month.
//
// "Owing" intentionally **does not** include the card's prior-month carryover
// or starting balance. It only reflects this month: total outflows on the
// card (charges + bank-booked interest) minus inflows from transfers in
// (covers + principal payments). Use this to know how much to pay on the
// card before the cycle closes.
type CreditActivity struct {
	AccountID      int64
	AccountName    string
	PurchasesCents int64
	PaymentsCents  int64
	OwingCents     int64
}

// CreditCardActivityForMonth returns one row per non-archived credit
// account, with this month's purchases (outflows) and payments (transfer
// inflows). Loans are excluded — they typically have a fixed payment plan
// rather than a revolving "what to pay this month" amount.
//
// When an account is part of the paydown plan and has a linked payment
// category, transfer-inflow transactions whose category matches that
// linked category are excluded from "payments". Those transfers are the
// projected paydown payment; counting them here as well would let the same
// payment push "Owing" negative (double-counting against the paydown
// projection). All other transfer inflows still count, so a regular
// transfer-to-credit without that category still reduces Owing.
func (s *Store) CreditCardActivityForMonth(ctx context.Context, userID int64, month string) ([]CreditActivity, error) {
	// Transfers store their category on the outgoing leg only — the
	// inflow row visible on the credit account has category_id = NULL but
	// transfer_pair_id pointing back to the outflow leg. COALESCE the two
	// so the filter sees the user-assigned category regardless of which
	// leg we're scanning.
	q := `
SELECT a.id, a.name,
  COALESCE((SELECT SUM(t.outflow_cents) FROM transactions t
            WHERE t.account_id = a.id
              AND to_char(t.date, 'YYYY-MM') = $1 AND t.user_id = $2), 0) AS purchases,
  COALESCE((SELECT SUM(t.inflow_cents) FROM transactions t
            LEFT JOIN transactions tp ON tp.id = t.transfer_pair_id AND tp.user_id = t.user_id
            WHERE t.account_id = a.id
              AND t.transfer_account_id IS NOT NULL
              AND to_char(t.date, 'YYYY-MM') = $3 AND t.user_id = $4
              AND (a.payment_category_id IS NULL
                   OR COALESCE(t.category_id, tp.category_id) IS NULL
                   OR COALESCE(t.category_id, tp.category_id) <> a.payment_category_id)), 0) AS payments
FROM accounts a
WHERE a.type = 'credit' AND a.archived_at IS NULL AND a.user_id = $5
ORDER BY a.name`
	rows, err := s.queryAll(ctx, q, month, userID, month, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("credit activity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CreditActivity
	for rows.Next() {
		var ca CreditActivity
		if err := rows.Scan(&ca.AccountID, &ca.AccountName, &ca.PurchasesCents, &ca.PaymentsCents); err != nil {
			return nil, err
		}
		ca.OwingCents = ca.PurchasesCents - ca.PaymentsCents
		out = append(out, ca)
	}
	return out, rows.Err()
}

// ActualIncomeForMonth sums the net inflow into income-flagged categories
// for the given YYYY-MM. Excludes transfers (inter-account moves aren't
// real income).
func (s *Store) ActualIncomeForMonth(ctx context.Context, userID int64, month string) (int64, error) {
	var v int64
	q := `
SELECT COALESCE(SUM(t.inflow_cents) - SUM(t.outflow_cents), 0)
FROM transactions t
JOIN categories c ON c.id = t.category_id AND c.user_id = t.user_id
WHERE c.is_income = $1
  AND t.transfer_account_id IS NULL
  AND to_char(t.date, 'YYYY-MM') = $2 AND t.user_id = $3`
	err := s.queryOne(ctx, q, true, month, userID).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// UncategorizedSpent sums net outflow (outflow − inflow) for the month across
// transactions with no category, excluding transfers (inter-account moves
// aren't spending). Used for the budget page's read-only Uncategorized row.
func (s *Store) UncategorizedSpent(ctx context.Context, userID int64, month string) (int64, error) {
	var v int64
	q := `
SELECT COALESCE(SUM(outflow_cents) - SUM(inflow_cents), 0)
FROM transactions
WHERE category_id IS NULL
  AND transfer_account_id IS NULL
  AND to_char(date, 'YYYY-MM') = $1 AND user_id = $2`
	if err := s.queryOne(ctx, q, month, userID).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// PaymentSource describes which datum supplied a month's paydown payment.
type PaymentSource int

const (
	PaymentDefault PaymentSource = iota
	PaymentAssigned
	PaymentSpent
)

func (p PaymentSource) String() string {
	switch p {
	case PaymentSpent:
		return "spent"
	case PaymentAssigned:
		return "assigned"
	case PaymentDefault:
		return "default"
	}
	return ""
}

// MonthPayment is one month of a payment schedule.
type MonthPayment struct {
	Month  string // YYYY-MM
	Cents  int64
	Source PaymentSource
}

// PaymentScheduleForCategory builds a per-month payment schedule for the next
// `months` months starting at start.
//
// For each month the value is chosen as:
//  1. spent   = SUM(outflow) - SUM(inflow) of transactions in that month with
//     category_id = categoryID, if > 0;
//  2. assigned in budgets table for that month, if > 0;
//  3. fallback (the account's default monthly payment).
//
// If categoryID is nil, every month falls back.
func (s *Store) PaymentScheduleForCategory(ctx context.Context, userID int64, categoryID *int64, start time.Time, months int, fallbackCents int64) ([]MonthPayment, error) {
	if months <= 0 {
		return nil, nil
	}
	out := make([]MonthPayment, 0, months)
	cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < months; i++ {
		key := cur.Format("2006-01")
		mp := MonthPayment{Month: key, Cents: fallbackCents, Source: PaymentDefault}

		if categoryID != nil {
			var spent int64
			q := `SELECT COALESCE(SUM(outflow_cents) - SUM(inflow_cents), 0)
				 FROM transactions
				 WHERE category_id = $1 AND to_char(date, 'YYYY-MM') = $2 AND user_id = $3`
			if err := s.queryOne(ctx, q, *categoryID, key, userID).Scan(&spent); err != nil {
				return nil, err
			}
			if spent > 0 {
				mp = MonthPayment{Month: key, Cents: spent, Source: PaymentSpent}
			} else {
				var assigned int64
				if err := s.queryOne(ctx,
					`SELECT COALESCE(assigned_cents, 0) FROM budgets WHERE month=$1 AND category_id=$2 AND user_id=$3`,
					key, *categoryID, userID).Scan(&assigned); err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				if assigned > 0 {
					mp = MonthPayment{Month: key, Cents: assigned, Source: PaymentAssigned}
				}
			}
		}
		out = append(out, mp)
		cur = cur.AddDate(0, 1, 0)
	}
	return out, nil
}

// nullableInt64 mirrors sql.NullInt64 with a shorter local name.
type nullableInt64 struct {
	Int64 int64
	Valid bool
}

func (n *nullableInt64) Scan(src any) error {
	if src == nil {
		n.Valid = false
		return nil
	}
	switch v := src.(type) {
	case int64:
		n.Int64, n.Valid = v, true
	case int:
		n.Int64, n.Valid = int64(v), true
	default:
		return fmt.Errorf("nullableInt64: unsupported type %T", src)
	}
	return nil
}
