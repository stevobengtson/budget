package billing

import (
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

func TestSubscriptionToStore(t *testing.T) {
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

	got := subscriptionToStore(7, sub)

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

// TestSubscriptionToStore_NoItems guards the nil-safe paths (a subscription
// payload without an items list or trial).
func TestSubscriptionToStore_NoItems(t *testing.T) {
	got := subscriptionToStore(1, &stripe.Subscription{ID: "sub_1", Status: stripe.SubscriptionStatusActive})
	if got.PriceID != "" || got.CurrentPeriodEnd != nil || got.TrialEnd != nil {
		t.Errorf("expected empty price/period/trial, got %+v", got)
	}
}
