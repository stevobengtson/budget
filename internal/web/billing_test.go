package web

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"

	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/store"
)

const testWebhookSecret = "whsec_test_secret"
const testBasePrice = "price_test_base"

// billingCfg returns a config with Stripe enabled so the subscription gate and
// webhook verification are live. The secret key is a dummy — these tests never
// call Stripe's API (the gate reads the store; the webhook is signed locally).
func billingCfg(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Stripe.SecretKey = "sk_test_dummy"
	cfg.Stripe.PriceIDs.Base = testBasePrice
	cfg.Stripe.WebhookSecret = testWebhookSecret
	return cfg
}

// TestSubscriptionGate verifies the gate blocks the core app without an active
// subscription, redirects to /billing (which stays reachable), and lets the app
// through once an active subscription exists.
func TestSubscriptionGate(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthedCfg(t, s, billingCfg(t))
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	// Never subscribed: /budget auto-redirects to /billing/start (opens the trial
	// Checkout). We stop at the redirect (CheckRedirect above), so no Stripe call.
	resp, _ := client.Get(ts.URL + "/budget")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/billing/start" {
		t.Fatalf("never-subscribed /budget = %d %q, want 303 /billing/start", resp.StatusCode, resp.Header.Get("Location"))
	}

	// A lapsed (canceled) subscription goes to the manual /billing page instead —
	// expired users aren't auto-thrown at a payment page.
	if err := s.UpsertSubscription(context.Background(), store.Subscription{
		UserID: uid, StripeSubscriptionID: "sub_gone", StripeCustomerID: "cus_1",
		PriceID: testBasePrice, Status: "canceled",
	}); err != nil {
		t.Fatal(err)
	}
	resp, _ = client.Get(ts.URL + "/budget")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/billing" {
		t.Fatalf("lapsed /budget = %d %q, want 303 /billing", resp.StatusCode, resp.Header.Get("Location"))
	}

	// /billing itself is ungated and reachable while locked out.
	resp, _ = client.Get(ts.URL + "/billing")
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Subscribe") {
		t.Fatalf("/billing = %d, want 200 with a Subscribe CTA", resp.StatusCode)
	}

	// Grant an active subscription for the base price → /budget is served.
	if err := s.UpsertSubscription(context.Background(), store.Subscription{
		UserID: uid, StripeSubscriptionID: "sub_active", StripeCustomerID: "cus_1",
		PriceID: testBasePrice, Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	resp, _ = client.Get(ts.URL + "/budget")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribed /budget = %d, want 200", resp.StatusCode)
	}
}

// TestBillingExemptBypassesGate verifies a complimentary (billing_exempt)
// account — set manually in the DB — reaches the app with no subscription, and
// its /billing page shows the complimentary state instead of a trial CTA.
func TestBillingExemptBypassesGate(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthedCfg(t, s, billingCfg(t))
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	// Not exempt, no subscription → gated.
	resp, _ := client.Get(ts.URL + "/budget")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("non-exempt /budget = %d, want 303 (gated)", resp.StatusCode)
	}

	// Flip the flag the way the operator would in production.
	if _, err := s.DB().ExecContext(context.Background(),
		"UPDATE users SET billing_exempt = true WHERE id = $1", uid); err != nil {
		t.Fatal(err)
	}

	// Now the app is served with no subscription at all.
	resp, _ = client.Get(ts.URL + "/budget")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exempt /budget = %d, want 200", resp.StatusCode)
	}
	// And /billing reflects the complimentary status.
	body := readAll(t, mustGetOK(t, client, ts.URL+"/billing"))
	if !strings.Contains(body, "Complimentary access") {
		t.Error("exempt /billing should show the complimentary state")
	}
	if strings.Contains(body, "Start free trial") {
		t.Error("exempt /billing should not show a trial CTA")
	}
}

// TestBillingStandaloneLayout verifies a never-subscribed user's /billing is the
// chrome-less paywall, while a user with access sees the in-app management page
// inside the app shell. Regression for the early-return that skipped the
// Standalone flag for never-subscribed users.
//
// The marker is the nav rail itself rather than the account-overview widget:
// the widget used to live in the shell, but the redesign moved it into the
// individual pages, so it is no longer evidence either way.
func TestBillingStandaloneLayout(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthedCfg(t, s, billingCfg(t))

	// Never subscribed → standalone paywall, no app chrome.
	body := readAll(t, mustGetOK(t, client, ts.URL+"/billing"))
	if !strings.Contains(body, "Start free trial") {
		t.Error("standalone /billing missing Start free trial CTA")
	}
	if strings.Contains(body, `id="app-rail"`) {
		t.Error("standalone /billing should not render the app shell")
	}

	// Active subscription → in-app management page, with the sidebar.
	if err := s.UpsertSubscription(context.Background(), store.Subscription{
		UserID: uid, StripeSubscriptionID: "sub_mgmt", StripeCustomerID: "cus_1",
		PriceID: testBasePrice, Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	body = readAll(t, mustGetOK(t, client, ts.URL+"/billing"))
	if !strings.Contains(body, `id="app-rail"`) {
		t.Error("in-app /billing (active sub) should render the app shell")
	}
}

func mustGetOK(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	return resp
}

// TestOverviewNotGated pins the redirect-loop fix: the sidebar account-overview
// widget loads on /billing (which a locked-out user sees), so an unsubscribed
// user's HTMX request for it must return 200 — not an HX-Redirect to /billing,
// which would loop the page reload.
func TestOverviewNotGated(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthedCfg(t, s, billingCfg(t))
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/accounts/overview", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unsubscribed /accounts/overview = %d (HX-Redirect %q), want 200 with no redirect loop",
			resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
	if loc := resp.Header.Get("HX-Redirect"); loc != "" {
		t.Fatalf("overview issued HX-Redirect %q — would loop the /billing reload", loc)
	}
}

// TestStripeWebhookUpsertsSubscription posts a locally-signed subscription event
// to the public webhook endpoint and asserts the row is synced to the store,
// attributed via the user_id we stamp in subscription metadata.
func TestStripeWebhookUpsertsSubscription(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, uid := serveAuthedCfg(t, s, billingCfg(t))

	trialEnd := time.Now().Add(35 * 24 * time.Hour).Unix()
	periodEnd := time.Now().Add(35 * 24 * time.Hour).Unix()
	payload := fmt.Sprintf(`{
		"id":"evt_1","object":"event","api_version":%q,
		"type":"customer.subscription.created",
		"data":{"object":{
			"id":"sub_hook","object":"subscription","status":"trialing","currency":"cad",
			"cancel_at_period_end":false,"trial_end":%d,"customer":"cus_hook",
			"metadata":{"user_id":"%d"},
			"items":{"object":"list","data":[
				{"id":"si_1","object":"subscription_item","current_period_end":%d,"price":{"id":%q}}
			]}
		}}
	}`, stripe.APIVersion, trialEnd, uid, periodEnd, testBasePrice)

	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: []byte(payload),
		Secret:  testWebhookSecret,
	})

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/stripe", bytes.NewReader(signed.Payload))
	req.Header.Set("Stripe-Signature", signed.Header)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook status = %d, want 200", resp.StatusCode)
	}

	sub, err := s.GetSubscriptionForUser(context.Background(), uid, testBasePrice)
	if err != nil {
		t.Fatalf("subscription not synced: %v", err)
	}
	if sub.Status != "trialing" || sub.Currency != "cad" || sub.StripeSubscriptionID != "sub_hook" {
		t.Errorf("synced sub = %+v, want trialing/cad/sub_hook", sub)
	}
	if sub.TrialEnd == nil || sub.CurrentPeriodEnd == nil {
		t.Errorf("expected trial_end and current_period_end set, got %+v", sub)
	}
}

// TestStripeWebhookRejectsBadSignature ensures an unsigned/forged body is a 400.
func TestStripeWebhookRejectsBadSignature(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, billingCfg(t))

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhooks/stripe",
		bytes.NewReader([]byte(`{"object":"event","type":"customer.subscription.created"}`)))
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged webhook status = %d, want 400", resp.StatusCode)
	}
}
