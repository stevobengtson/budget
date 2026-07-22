package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Subscription mirrors a Stripe subscription for one user. Status is Stripe's
// status string verbatim (trialing, active, past_due, canceled, ...).
type Subscription struct {
	ID                   int64
	UserID               int64
	StripeSubscriptionID string
	StripeCustomerID     string
	PriceID              string
	Status               string
	Currency             string
	TrialEnd             *time.Time
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// accessGrantingStatuses are the Stripe subscription statuses that grant app
// access: an active trial, a paid subscription, or one mid-dunning (Stripe is
// still retrying payment, so we keep the grace window open).
var accessGrantingStatuses = map[string]bool{
	"trialing": true,
	"active":   true,
	"past_due": true,
}

// AccessGranting reports whether a subscription in the given Stripe status
// should grant the user access to the app.
func AccessGranting(status string) bool { return accessGrantingStatuses[status] }

// IsBillingExempt reports whether the user is a complimentary / always-free
// account that bypasses the subscription gate. It's set manually in the database
// (there's no UI): UPDATE users SET billing_exempt = true WHERE email = '...'.
func (s *Store) IsBillingExempt(ctx context.Context, userID int64) (bool, error) {
	var exempt bool
	if err := s.queryOne(ctx, `SELECT billing_exempt FROM users WHERE id = $1`, userID).Scan(&exempt); err != nil {
		return false, err
	}
	return exempt, nil
}

// SetUserStripeCustomer records the user's Stripe customer id (set the first
// time we create a customer for them).
func (s *Store) SetUserStripeCustomer(ctx context.Context, userID int64, customerID string) error {
	_, err := s.run(ctx,
		`UPDATE users SET stripe_customer_id = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		customerID, userID)
	return err
}

// GetUserStripeCustomer returns the user's Stripe customer id, or "" if they
// don't have one yet.
func (s *Store) GetUserStripeCustomer(ctx context.Context, userID int64) (string, error) {
	var cid sql.NullString
	if err := s.queryOne(ctx, `SELECT stripe_customer_id FROM users WHERE id = $1`, userID).Scan(&cid); err != nil {
		return "", err
	}
	return cid.String, nil
}

// GetUserByStripeCustomer resolves the user owning a Stripe customer id, used to
// attribute incoming webhook events. Returns sql.ErrNoRows when unknown.
func (s *Store) GetUserByStripeCustomer(ctx context.Context, customerID string) (User, error) {
	return s.scanUser(s.queryOne(ctx,
		`SELECT id, email, name, password_hash, email_verified_at, avatar_updated_at, created_at, updated_at
		 FROM users WHERE stripe_customer_id = $1`, customerID))
}

// UpsertSubscription inserts or updates a subscription keyed by its Stripe id,
// so replaying a webhook (or receiving events out of order) converges on the
// latest state rather than duplicating rows.
func (s *Store) UpsertSubscription(ctx context.Context, sub Subscription) error {
	_, err := s.run(ctx,
		`INSERT INTO subscriptions
		   (user_id, stripe_subscription_id, stripe_customer_id, price_id, status,
		    currency, trial_end, current_period_end, cancel_at_period_end)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (stripe_subscription_id) DO UPDATE SET
		    status               = EXCLUDED.status,
		    price_id             = EXCLUDED.price_id,
		    currency             = EXCLUDED.currency,
		    trial_end            = EXCLUDED.trial_end,
		    current_period_end   = EXCLUDED.current_period_end,
		    cancel_at_period_end = EXCLUDED.cancel_at_period_end,
		    updated_at           = CURRENT_TIMESTAMP`,
		sub.UserID, sub.StripeSubscriptionID, sub.StripeCustomerID, sub.PriceID, sub.Status,
		sub.Currency, sub.TrialEnd, sub.CurrentPeriodEnd, sub.CancelAtPeriodEnd)
	if err != nil {
		return fmt.Errorf("upsert subscription %s: %w", sub.StripeSubscriptionID, err)
	}
	return nil
}

// GetSubscriptionForUser returns the user's subscription to priceID (the base
// plan), or sql.ErrNoRows when they have none. If more than one exists (e.g. a
// re-subscribe after cancellation), the most recent row wins.
func (s *Store) GetSubscriptionForUser(ctx context.Context, userID int64, priceID string) (Subscription, error) {
	return s.scanSubscription(s.queryOne(ctx,
		`SELECT id, user_id, stripe_subscription_id, stripe_customer_id, price_id, status,
		        currency, trial_end, current_period_end, cancel_at_period_end, created_at, updated_at
		 FROM subscriptions
		 WHERE user_id = $1 AND price_id = $2
		 ORDER BY created_at DESC
		 LIMIT 1`, userID, priceID))
}

func (s *Store) scanSubscription(row *sql.Row) (Subscription, error) {
	var sub Subscription
	var trialEnd, periodEnd nullTime
	if err := row.Scan(
		&sub.ID, &sub.UserID, &sub.StripeSubscriptionID, &sub.StripeCustomerID, &sub.PriceID,
		&sub.Status, &sub.Currency, &trialEnd, &periodEnd, &sub.CancelAtPeriodEnd,
		&sub.CreatedAt, &sub.UpdatedAt,
	); err != nil {
		return Subscription{}, err
	}
	sub.TrialEnd = trialEnd.Ptr()
	sub.CurrentPeriodEnd = periodEnd.Ptr()
	return sub, nil
}
