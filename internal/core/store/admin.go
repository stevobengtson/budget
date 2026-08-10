package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AdminUserRow is one line of the admin Users table: the user plus the derived
// state an operator needs to triage an account at a glance — whether they are
// verified, suspended, comped, and where their subscription stands.
type AdminUserRow struct {
	ID            int64
	Email         string
	Name          string
	CreatedAt     time.Time
	EmailVerified bool
	IsAdmin       bool
	DisabledAt    *time.Time
	// SubStatus is the Stripe status of the user's most recent subscription,
	// or "" when they have never had one.
	SubStatus string
	// CompActive reports an in-force complimentary account (billing_exempt with
	// an unexpired end date); CompUntil is that end date, nil for a lifetime comp.
	CompActive bool
	CompUntil  *time.Time
}

// ListUsers returns one page of users, newest signup first. The subscription
// column comes from a LATERAL join to the single most recent subscription row,
// which keeps this one query per page rather than one per user.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]AdminUserRow, error) {
	rows, err := s.queryAll(ctx,
		`SELECT u.id, u.email, u.name, u.created_at,
		        u.email_verified_at IS NOT NULL,
		        u.is_admin, u.disabled_at,
		        COALESCE(sub.status, ''),
		        u.billing_exempt AND (u.billing_exempt_until IS NULL OR u.billing_exempt_until > CURRENT_TIMESTAMP),
		        u.billing_exempt_until
		 FROM users u
		 LEFT JOIN LATERAL (
		     SELECT status FROM subscriptions
		     WHERE user_id = u.id
		     ORDER BY created_at DESC
		     LIMIT 1
		 ) sub ON true
		 ORDER BY u.created_at DESC, u.id DESC
		 LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AdminUserRow
	for rows.Next() {
		var r AdminUserRow
		var disabled, compUntil nullTime
		if err := rows.Scan(&r.ID, &r.Email, &r.Name, &r.CreatedAt, &r.EmailVerified,
			&r.IsAdmin, &disabled, &r.SubStatus, &r.CompActive, &compUntil); err != nil {
			return nil, err
		}
		r.DisabledAt = disabled.Ptr()
		r.CompUntil = compUntil.Ptr()
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetUserDisabled suspends or restores an account. Suspending also deletes the
// user's sessions so the lockout is immediate rather than taking effect on their
// next session lookup; both happen in one transaction so a session cannot
// survive a committed disable.
func (s *Store) SetUserDisabled(ctx context.Context, userID int64, disabled bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if disabled {
		if _, err := s.txExec(ctx, tx,
			`UPDATE users SET disabled_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
			userID); err != nil {
			return fmt.Errorf("disable user: %w", err)
		}
		if _, err := s.txExec(ctx, tx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
	} else if _, err := s.txExec(ctx, tx,
		`UPDATE users SET disabled_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
		userID); err != nil {
		return fmt.Errorf("enable user: %w", err)
	}
	return tx.Commit()
}

// GrantComp gives the user a complimentary subscription. until is the end of the
// comp, or nil for a lifetime one. It does not touch Stripe: a comp only
// bypasses the app's access gate, so a user who is also paying keeps their
// Stripe subscription and should cancel it themselves.
func (s *Store) GrantComp(ctx context.Context, userID int64, until *time.Time) error {
	_, err := s.run(ctx,
		`UPDATE users SET billing_exempt = true, billing_exempt_until = $1, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $2`, until, userID)
	if err != nil {
		return fmt.Errorf("grant comp: %w", err)
	}
	return nil
}

// RevokeComp removes a complimentary subscription, dropping the user back to
// whatever their Stripe subscription grants (possibly nothing).
func (s *Store) RevokeComp(ctx context.Context, userID int64) error {
	_, err := s.run(ctx,
		`UPDATE users SET billing_exempt = false, billing_exempt_until = NULL, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("revoke comp: %w", err)
	}
	return nil
}

// GetComp returns the user's raw complimentary-account state: whether the flag
// is set at all, and its end date (nil = no expiry). Note that flagSet can be
// true while the comp has already lapsed — use IsBillingExempt for the effective
// answer; this is for showing an admin what is actually stored.
func (s *Store) GetComp(ctx context.Context, userID int64) (flagSet bool, until *time.Time, err error) {
	var t nullTime
	if err := s.queryOne(ctx,
		`SELECT billing_exempt, billing_exempt_until FROM users WHERE id = $1`, userID).
		Scan(&flagSet, &t); err != nil {
		return false, nil, err
	}
	return flagSet, t.Ptr(), nil
}

// SetUserAdmin grants or revokes the admin flag.
func (s *Store) SetUserAdmin(ctx context.Context, userID int64, admin bool) error {
	_, err := s.run(ctx,
		`UPDATE users SET is_admin = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, admin, userID)
	return err
}

// HasAccessGrantingSubscription reports whether any of the user's subscriptions
// is in a status that still grants access — which, for the admin console, means
// Stripe may still be billing them. It reads the statuses and evaluates them
// through AccessGranting rather than repeating the list in SQL, so the two
// cannot drift apart.
func (s *Store) HasAccessGrantingSubscription(ctx context.Context, userID int64) (bool, error) {
	rows, err := s.queryAll(ctx, `SELECT status FROM subscriptions WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return false, err
		}
		if AccessGranting(status) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ListSubscriptionsForUser returns every subscription row the user has, newest
// first, for the admin user detail page.
func (s *Store) ListSubscriptionsForUser(ctx context.Context, userID int64) ([]Subscription, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, user_id, stripe_subscription_id, stripe_customer_id, price_id, status,
		        currency, trial_end, current_period_end, cancel_at_period_end, created_at, updated_at
		 FROM subscriptions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Subscription
	for rows.Next() {
		var sub Subscription
		var trialEnd, periodEnd nullTime
		if err := rows.Scan(
			&sub.ID, &sub.UserID, &sub.StripeSubscriptionID, &sub.StripeCustomerID, &sub.PriceID,
			&sub.Status, &sub.Currency, &trialEnd, &periodEnd, &sub.CancelAtPeriodEnd,
			&sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, err
		}
		sub.TrialEnd = trialEnd.Ptr()
		sub.CurrentPeriodEnd = periodEnd.Ptr()
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SignupBucket is one point on the admin dashboard's signups chart.
type SignupBucket struct {
	Start time.Time
	Count int
}

// SignupUnit is the granularity of a signups series.
type SignupUnit string

const (
	SignupDaily   SignupUnit = "day"
	SignupMonthly SignupUnit = "month"
)

// SignupSeries returns exactly n consecutive buckets ending with the one that
// contains now, oldest first, zero-filled — so the chart always has a fixed
// number of points and quiet periods read as flat rather than vanishing.
//
// Bucketing is done in UTC (created_at is TIMESTAMPTZ, and a bare date_trunc
// would silently follow the session time zone, shifting the boundaries), and the
// Go-side bucket starts are computed the same way so the two line up.
func (s *Store) SignupSeries(ctx context.Context, unit SignupUnit, n int) ([]SignupBucket, error) {
	if n <= 0 {
		return nil, nil
	}
	series := make([]SignupBucket, n)
	now := time.Now().UTC()
	for i := range series {
		series[i].Start = bucketStart(unit, now, -(n - 1 - i))
	}

	// The bucket comes back as a 'YYYY-MM-DD' string rather than a timestamp: it
	// is matched against a Go-formatted key, so the pairing cannot depend on
	// which time.Location the driver picks when scanning a zone-less timestamp.
	// Monthly buckets are always day 01, so one format serves both units.
	rows, err := s.queryAll(ctx,
		`SELECT to_char(date_trunc($1, created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS bucket, COUNT(*)
		 FROM users
		 WHERE created_at >= $2
		 GROUP BY bucket`, string(unit), series[0].Start)
	if err != nil {
		return nil, fmt.Errorf("signup series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	index := make(map[string]int, n)
	for i, b := range series {
		index[b.Start.Format(bucketKeyLayout)] = i
	}
	for rows.Next() {
		var bucket string
		var count int
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		if i, ok := index[bucket]; ok {
			series[i].Count = count
		}
	}
	return series, rows.Err()
}

// bucketKeyLayout matches the to_char format used to group signups.
const bucketKeyLayout = "2006-01-02"

// bucketStart returns the UTC start of the bucket that is `offset` units away
// from the one containing t (offset is negative to go back in time).
func bucketStart(unit SignupUnit, t time.Time, offset int) time.Time {
	if unit == SignupMonthly {
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, offset, 0)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, offset)
}

// AdminStats is the dashboard's set of headline counters.
type AdminStats struct {
	TotalUsers int
	ActiveSubs int
	Comped     int
	Disabled   int
}

// CountAdminStats gathers the dashboard counters in one round trip. Active subs
// are counted from distinct users so a re-subscriber is not double counted, and
// the statuses that count as active are bound from AccessGrantingStatuses rather
// than written out here, so the dashboard cannot disagree with the access gate.
func (s *Store) CountAdminStats(ctx context.Context) (AdminStats, error) {
	statuses := AccessGrantingStatuses()
	args := make([]any, len(statuses))
	placeholders := make([]string, len(statuses))
	for i, st := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = st
	}

	var st AdminStats
	err := s.queryOne(ctx,
		`SELECT
		   (SELECT COUNT(*) FROM users),
		   (SELECT COUNT(DISTINCT user_id) FROM subscriptions WHERE status IN (`+strings.Join(placeholders, ",")+`)),
		   (SELECT COUNT(*) FROM users
		      WHERE billing_exempt AND (billing_exempt_until IS NULL OR billing_exempt_until > CURRENT_TIMESTAMP)),
		   (SELECT COUNT(*) FROM users WHERE disabled_at IS NOT NULL)`, args...).
		Scan(&st.TotalUsers, &st.ActiveSubs, &st.Comped, &st.Disabled)
	if err != nil {
		return AdminStats{}, fmt.Errorf("admin stats: %w", err)
	}
	return st, nil
}
