package billing

import (
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

func TestSubscriptionToStoreRows(t *testing.T) {
	periodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
	trialEnd := time.Now().Add(20 * 24 * time.Hour).Unix()

	sub := &stripe.Subscription{
		ID:                "sub_abc",
		Status:            stripe.SubscriptionStatusTrialing,
		Currency:          stripe.CurrencyCAD,
		CancelAtPeriodEnd: true,
		TrialEnd:          trialEnd,
		Customer:          &stripe.Customer{ID: "cus_xyz"},
		Metadata:          map[string]string{"user_id": "42"},
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{{
				Price:            &stripe.Price{ID: "price_base"},
				CurrentPeriodEnd: periodEnd,
			}},
		},
	}

	rows := subscriptionToStoreRows(7, sub)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]

	if got.UserID != 7 || got.StripeSubscriptionID != "sub_abc" || got.StripeCustomerID != "cus_xyz" {
		t.Errorf("ids wrong: %+v", got)
	}
	if got.PriceID != "price_base" {
		t.Errorf("price id = %q, want price_base (from item)", got.PriceID)
	}
	if got.Status != "trialing" || got.Currency != "cad" || !got.CancelAtPeriodEnd {
		t.Errorf("status/currency/cancel wrong: %+v", got)
	}
	if got.TrialEnd == nil || got.TrialEnd.Unix() != trialEnd {
		t.Errorf("trial end = %v, want unix %d", got.TrialEnd, trialEnd)
	}
	if got.CurrentPeriodEnd == nil || got.CurrentPeriodEnd.Unix() != periodEnd {
		t.Errorf("period end = %v, want unix %d (from item)", got.CurrentPeriodEnd, periodEnd)
	}
}

// TestSubscriptionToStoreRows_MultiItem covers the base-plan-plus-add-on shape:
// one row per item, per-item price/period, shared subscription-level state.
func TestSubscriptionToStoreRows_MultiItem(t *testing.T) {
	basePeriodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
	addonPeriodEnd := time.Now().Add(15 * 24 * time.Hour).Unix()

	sub := &stripe.Subscription{
		ID:       "sub_multi",
		Status:   stripe.SubscriptionStatusActive,
		Currency: stripe.CurrencyCAD,
		Customer: &stripe.Customer{ID: "cus_xyz"},
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{Price: &stripe.Price{ID: "price_base"}, CurrentPeriodEnd: basePeriodEnd},
				{Price: &stripe.Price{ID: "price_bank_sync"}, CurrentPeriodEnd: addonPeriodEnd},
			},
		},
	}

	rows := subscriptionToStoreRows(7, sub)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row.StripeSubscriptionID != "sub_multi" || row.Status != "active" || row.StripeCustomerID != "cus_xyz" {
			t.Errorf("shared state wrong: %+v", row)
		}
	}
	if rows[0].PriceID != "price_base" || rows[0].CurrentPeriodEnd == nil || rows[0].CurrentPeriodEnd.Unix() != basePeriodEnd {
		t.Errorf("base row wrong: %+v", rows[0])
	}
	if rows[1].PriceID != "price_bank_sync" || rows[1].CurrentPeriodEnd == nil || rows[1].CurrentPeriodEnd.Unix() != addonPeriodEnd {
		t.Errorf("add-on row wrong: %+v", rows[1])
	}
}

// TestSubscriptionToStoreRows_NoItems guards the nil-safe paths (a subscription
// payload without an items list or trial still yields one status-bearing row).
func TestSubscriptionToStoreRows_NoItems(t *testing.T) {
	rows := subscriptionToStoreRows(1, &stripe.Subscription{ID: "sub_1", Status: stripe.SubscriptionStatusActive})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.PriceID != "" || got.CurrentPeriodEnd != nil || got.TrialEnd != nil {
		t.Errorf("expected empty price/period/trial, got %+v", got)
	}
}
