package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestSubscriptionsCustomerAndUpsert(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	const priceID = "price_base_test"

	// No customer, no subscription to start.
	if cid, err := s.GetUserStripeCustomer(ctx, uid); err != nil || cid != "" {
		t.Fatalf("customer id = %q err %v, want empty", cid, err)
	}
	if _, err := s.GetSubscriptionForUser(ctx, uid, priceID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSubscriptionForUser err = %v, want sql.ErrNoRows", err)
	}

	// Link a customer and resolve back to the user.
	if err := s.SetUserStripeCustomer(ctx, uid, "cus_123"); err != nil {
		t.Fatal(err)
	}
	if cid, _ := s.GetUserStripeCustomer(ctx, uid); cid != "cus_123" {
		t.Errorf("customer id = %q, want cus_123", cid)
	}
	if u, err := s.GetUserByStripeCustomer(ctx, "cus_123"); err != nil || u.ID != uid {
		t.Errorf("GetUserByStripeCustomer = %d err %v, want %d", u.ID, err, uid)
	}

	// Insert a trialing subscription, then upsert it to active — same row, new state.
	trialEnd := time.Now().Add(35 * 24 * time.Hour).UTC().Truncate(time.Second)
	sub := Subscription{
		UserID: uid, StripeSubscriptionID: "sub_1", StripeCustomerID: "cus_123",
		PriceID: priceID, Status: "trialing", Currency: "usd", TrialEnd: &trialEnd,
	}
	if err := s.UpsertSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSubscriptionForUser(ctx, uid, priceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "trialing" || got.TrialEnd == nil || !got.TrialEnd.Equal(trialEnd) {
		t.Errorf("after insert: status=%q trialEnd=%v, want trialing / %v", got.Status, got.TrialEnd, trialEnd)
	}

	sub.Status = "active"
	sub.Currency = "cad"
	if err := s.UpsertSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSubscriptionForUser(ctx, uid, priceID)
	if got.Status != "active" || got.Currency != "cad" {
		t.Errorf("after upsert: status=%q currency=%q, want active / cad", got.Status, got.Currency)
	}

	// Access policy.
	for status, want := range map[string]bool{
		"trialing": true, "active": true, "past_due": true,
		"canceled": false, "unpaid": false, "incomplete_expired": false, "paused": false,
	} {
		if AccessGranting(status) != want {
			t.Errorf("AccessGranting(%q) = %v, want %v", status, AccessGranting(status), want)
		}
	}
}

// TestSubscriptionMultiItemRows exercises the one-row-per-item model: two
// prices on the same Stripe subscription coexist, and DeleteSubscriptionRowsExcept
// prunes a detached item's row while keeping the rest.
func TestSubscriptionMultiItemRows(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	base := Subscription{
		UserID: uid, StripeSubscriptionID: "sub_multi", StripeCustomerID: "cus_m",
		PriceID: "price_base", Status: "active",
	}
	addon := base
	addon.PriceID = "price_bank_sync"
	if err := s.UpsertSubscription(ctx, base); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSubscription(ctx, addon); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSubscriptionForUser(ctx, uid, "price_base"); err != nil {
		t.Fatalf("base row missing: %v", err)
	}
	if _, err := s.GetSubscriptionForUser(ctx, uid, "price_bank_sync"); err != nil {
		t.Fatalf("add-on row missing: %v", err)
	}

	// Detach the add-on item: keep only the base price.
	if err := s.DeleteSubscriptionRowsExcept(ctx, "sub_multi", []string{"price_base"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSubscriptionForUser(ctx, uid, "price_bank_sync"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("add-on row err = %v, want sql.ErrNoRows after prune", err)
	}
	if _, err := s.GetSubscriptionForUser(ctx, uid, "price_base"); err != nil {
		t.Fatalf("base row lost by prune: %v", err)
	}

	// Empty keep list removes every row of the subscription.
	if err := s.DeleteSubscriptionRowsExcept(ctx, "sub_multi", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSubscriptionForUser(ctx, uid, "price_base"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("base row err = %v, want sql.ErrNoRows after full prune", err)
	}
}
