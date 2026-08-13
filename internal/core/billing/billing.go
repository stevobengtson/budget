// Package billing wraps Stripe: creating customers, starting the trial via
// Checkout, opening the Customer Portal, and turning subscription webhooks into
// rows in the store. All money state of record lives in Stripe; the store is a
// synced projection used for the app's access gate.
package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"

	"github.com/sbengtson/budget/internal/core/store"
)

// TrialPeriodDays is the free-trial length for the base plan.
const TrialPeriodDays = 35

// ErrNotConfigured is returned by operations that need Stripe when the secret
// key or base price id is unset (billing disabled for this environment).
var ErrNotConfigured = errors.New("billing is not configured")

// errUnknownUser means a webhook subscription could not be attributed to a user.
var errUnknownUser = errors.New("no user for subscription")

// Service holds a Stripe client scoped to one account plus the store it syncs.
type Service struct {
	sc            *stripe.Client
	store         *store.Store
	basePrice     string
	bankSyncPrice string
	baseURL       string
	webhookSecret string
	configured    bool
}

// NewService builds the billing service. When secretKey or basePriceID is empty
// the service is inert (Enabled reports false) so the app runs without Stripe.
// bankSyncPriceID is the bank_sync add-on's Stripe Price; empty means the
// add-on is not billed (dev, or billing off).
func NewService(st *store.Store, secretKey, basePriceID, bankSyncPriceID, baseURL, webhookSecret string) *Service {
	return &Service{
		sc:            stripe.NewClient(secretKey),
		store:         st,
		basePrice:     basePriceID,
		bankSyncPrice: bankSyncPriceID,
		baseURL:       baseURL,
		webhookSecret: webhookSecret,
		configured:    secretKey != "" && basePriceID != "",
	}
}

// BankSyncEntitled reports whether the user may actually use bank sync right
// now: the add-on toggled on, plus a billing story that covers it — billing
// disabled entirely (dev), a comped account, an unpriced add-on, or an
// access-granting subscription row for the add-on's price. Shared by the
// webhook handler and the polling worker so a lapsed subscription stops syncs
// everywhere at once.
func (s *Service) BankSyncEntitled(ctx context.Context, userID int64) bool {
	slugs, err := s.store.EnabledAddOnSlugs(ctx, userID)
	if err != nil || !slices.Contains(slugs, "bank_sync") {
		return false
	}
	if !s.configured || s.bankSyncPrice == "" {
		return true
	}
	if exempt, err := s.store.IsBillingExempt(ctx, userID); err == nil && exempt {
		return true
	}
	sub, err := s.store.GetSubscriptionForUser(ctx, userID, s.bankSyncPrice)
	return err == nil && store.AccessGranting(sub.Status)
}

// Enabled reports whether billing is configured (and therefore the access gate
// and billing UI are live).
func (s *Service) Enabled() bool { return s.configured }

// BasePriceID is the Stripe Price id of the base plan, used by the access gate
// to find the user's base subscription.
func (s *Service) BasePriceID() string { return s.basePrice }

// ensureCustomer returns the user's Stripe customer id, creating and persisting
// one on first use.
func (s *Service) ensureCustomer(ctx context.Context, userID int64, email, name string) (string, error) {
	cid, err := s.store.GetUserStripeCustomer(ctx, userID)
	if err != nil {
		return "", err
	}
	if cid != "" {
		return cid, nil
	}
	cp := &stripe.CustomerCreateParams{Email: stripe.String(email)}
	if name != "" {
		cp.Name = stripe.String(name)
	}
	cp.AddMetadata("user_id", strconv.FormatInt(userID, 10))
	cust, err := s.sc.V1Customers.Create(ctx, cp)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	if err := s.store.SetUserStripeCustomer(ctx, userID, cust.ID); err != nil {
		return "", err
	}
	return cust.ID, nil
}

// StartCheckoutURL creates a Checkout Session for the base plan and returns its
// hosted URL. A user who has never subscribed gets the 35-day no-card trial
// (payment_method_collection=if_required); a returning user (trial already
// used) is charged immediately (payment_method_collection=always).
func (s *Service) StartCheckoutURL(ctx context.Context, userID int64, email, name string) (string, error) {
	if !s.configured {
		return "", ErrNotConfigured
	}
	customerID, err := s.ensureCustomer(ctx, userID, email, name)
	if err != nil {
		return "", err
	}

	// A prior subscription row (even canceled) means the trial was already used.
	firstTime := false
	if _, err := s.store.GetSubscriptionForUser(ctx, userID, s.basePrice); errors.Is(err, sql.ErrNoRows) {
		firstTime = true
	}

	uidStr := strconv.FormatInt(userID, 10)
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:          stripe.String(customerID),
		ClientReferenceID: stripe.String(uidStr),
		SuccessURL:        stripe.String(s.baseURL + "/billing/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:         stripe.String(s.baseURL + "/billing"),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Price:    stripe.String(s.basePrice),
			Quantity: stripe.Int64(1),
		}},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{"user_id": uidStr},
		},
	}
	if firstTime {
		params.PaymentMethodCollection = stripe.String(string(stripe.CheckoutSessionPaymentMethodCollectionIfRequired))
		params.SubscriptionData.TrialPeriodDays = stripe.Int64(TrialPeriodDays)
		params.SubscriptionData.TrialSettings = &stripe.CheckoutSessionCreateSubscriptionDataTrialSettingsParams{
			EndBehavior: &stripe.CheckoutSessionCreateSubscriptionDataTrialSettingsEndBehaviorParams{
				MissingPaymentMethod: stripe.String("cancel"),
			},
		}
	} else {
		params.PaymentMethodCollection = stripe.String(string(stripe.CheckoutSessionPaymentMethodCollectionAlways))
	}

	sess, err := s.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return sess.URL, nil
}

// PortalURL opens the Stripe Customer Portal for the user to add a card, change,
// or cancel their subscription. Returns ErrNotConfigured when billing is off or
// the user has no Stripe customer yet.
func (s *Service) PortalURL(ctx context.Context, userID int64) (string, error) {
	if !s.configured {
		return "", ErrNotConfigured
	}
	cid, err := s.store.GetUserStripeCustomer(ctx, userID)
	if err != nil {
		return "", err
	}
	if cid == "" {
		return "", ErrNotConfigured
	}
	ps, err := s.sc.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(cid),
		ReturnURL: stripe.String(s.baseURL + "/account"),
	})
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return ps.URL, nil
}

// CancelAtPeriodEnd flags the user's base subscription to stop renewing at the
// end of the current billing period: no further charges, but access remains
// until the period ends. Used when a user deletes their account. It's a no-op
// when billing is off, the user has no tracked subscription, or the subscription
// is already in a non-granting (e.g. canceled) state.
func (s *Service) CancelAtPeriodEnd(ctx context.Context, userID int64) error {
	if !s.configured {
		return nil
	}
	sub, err := s.store.GetSubscriptionForUser(ctx, userID, s.basePrice)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sub.StripeSubscriptionID == "" || !store.AccessGranting(sub.Status) {
		return nil
	}
	if _, err := s.sc.V1Subscriptions.Update(ctx, sub.StripeSubscriptionID,
		&stripe.SubscriptionUpdateParams{CancelAtPeriodEnd: stripe.Bool(true)}); err != nil {
		return fmt.Errorf("cancel subscription at period end: %w", err)
	}
	return nil
}

// ErrNoBaseSubscription is returned when a paid add-on is toggled on without
// an access-granting base subscription to attach its item to.
var ErrNoBaseSubscription = errors.New("no active base subscription")

// AttachAddOnItem adds the priced add-on as a second item on the user's base
// subscription, prorated onto the next invoice. Idempotent: an already
// attached, access-granting add-on row is a no-op, so a replayed toggle can't
// double-bill. The store is synced immediately (not left to the webhook) so
// the entitlement check sees the new item on the very next request.
func (s *Service) AttachAddOnItem(ctx context.Context, userID int64, priceID string) error {
	if !s.configured || priceID == "" {
		return ErrNotConfigured
	}
	base, err := s.store.GetSubscriptionForUser(ctx, userID, s.basePrice)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoBaseSubscription
	}
	if err != nil {
		return err
	}
	if !store.AccessGranting(base.Status) {
		return ErrNoBaseSubscription
	}
	if existing, err := s.store.GetSubscriptionForUser(ctx, userID, priceID); err == nil && store.AccessGranting(existing.Status) {
		return nil
	}
	if _, err := s.sc.V1SubscriptionItems.Create(ctx, &stripe.SubscriptionItemCreateParams{
		Subscription:      stripe.String(base.StripeSubscriptionID),
		Price:             stripe.String(priceID),
		Quantity:          stripe.Int64(1),
		ProrationBehavior: stripe.String("create_prorations"),
	}); err != nil {
		return fmt.Errorf("attach add-on item: %w", err)
	}
	return s.resyncSubscription(ctx, base.StripeSubscriptionID)
}

// DetachAddOnItem removes the add-on's item from the user's base subscription,
// crediting unused time on the next invoice. A missing item is a no-op (the
// user may be exempt, or the webhook already converged the detach).
func (s *Service) DetachAddOnItem(ctx context.Context, userID int64, priceID string) error {
	if !s.configured || priceID == "" {
		return nil
	}
	row, err := s.store.GetSubscriptionForUser(ctx, userID, priceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sub, err := s.sc.V1Subscriptions.Retrieve(ctx, row.StripeSubscriptionID, nil)
	if err != nil {
		return fmt.Errorf("retrieve subscription: %w", err)
	}
	for _, item := range sub.Items.Data {
		if item.Price == nil || item.Price.ID != priceID {
			continue
		}
		if _, err := s.sc.V1SubscriptionItems.Delete(ctx, item.ID, &stripe.SubscriptionItemDeleteParams{
			ProrationBehavior: stripe.String("create_prorations"),
		}); err != nil {
			return fmt.Errorf("detach add-on item: %w", err)
		}
		break
	}
	return s.resyncSubscription(ctx, row.StripeSubscriptionID)
}

// resyncSubscription re-fetches a subscription from Stripe and converges the
// store's rows, closing the same API-call-vs-webhook race SyncCheckoutSession
// closes for checkout.
func (s *Service) resyncSubscription(ctx context.Context, stripeSubID string) error {
	sub, err := s.sc.V1Subscriptions.Retrieve(ctx, stripeSubID, nil)
	if err != nil {
		return fmt.Errorf("resync subscription: %w", err)
	}
	return s.syncSubscription(ctx, sub)
}

// BankSyncPriced reports whether the bank_sync add-on carries a real Stripe
// price in this environment (drives the paid-toggle path in ToggleAddOn).
func (s *Service) BankSyncPriced() bool { return s.configured && s.bankSyncPrice != "" }

// BankSyncPriceID exposes the add-on's price id for the toggle handler.
func (s *Service) BankSyncPriceID() string { return s.bankSyncPrice }

// SyncCheckoutSession retrieves a completed Checkout Session (with its
// subscription expanded) and upserts the resulting subscription immediately.
// Called from the success redirect so access is granted without waiting for the
// webhook (closing the redirect-vs-webhook race).
func (s *Service) SyncCheckoutSession(ctx context.Context, sessionID string) error {
	if !s.configured {
		return ErrNotConfigured
	}
	p := &stripe.CheckoutSessionRetrieveParams{}
	p.AddExpand("subscription")
	sess, err := s.sc.V1CheckoutSessions.Retrieve(ctx, sessionID, p)
	if err != nil {
		return fmt.Errorf("get checkout session: %w", err)
	}
	if sess.Subscription == nil {
		return nil // not a subscription checkout, or not completed yet
	}
	return s.syncSubscription(ctx, sess.Subscription)
}

// HandleWebhook verifies the Stripe signature on a raw webhook request and
// processes the event. A bad signature returns an error (respond 400); an
// unhandled event type is a no-op success.
//
// IgnoreAPIVersionMismatch is set because the webhook endpoint's (or the Stripe
// CLI's) API version rarely matches the SDK's pinned version, and the fields we
// read (status, trial_end, item price/current_period_end, customer, metadata)
// are stable across recent versions. The signature is still fully verified.
func (s *Service) HandleWebhook(payload []byte, sigHeader string) error {
	event, err := webhook.ConstructEventWithOptions(payload, sigHeader, s.webhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return fmt.Errorf("verify webhook signature: %w", err)
	}
	return s.processEvent(context.Background(), event)
}

// processEvent routes a verified event. Subscription lifecycle events are the
// source of truth for status; checkout completion is handled defensively (the
// success redirect usually syncs first). Other subscribed events are no-ops for
// now (Stripe handles trial-ending and dunning emails).
func (s *Service) processEvent(ctx context.Context, event stripe.Event) error {
	switch event.Type {
	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return fmt.Errorf("parse subscription event: %w", err)
		}
		return s.syncSubscription(ctx, &sub)
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return fmt.Errorf("parse checkout session event: %w", err)
		}
		if sess.Subscription == nil {
			return nil
		}
		return s.SyncCheckoutSession(ctx, sess.ID)
	default:
		return nil
	}
}

// syncSubscription attributes a Stripe subscription to a user and upserts it.
// An unattributable subscription is treated as a no-op success rather than an
// error: after a user deletes their account, Stripe still emits trailing events
// (e.g. the period-end subscription.deleted) for a subscription whose user is
// gone, and those must not fail the webhook and trigger endless Stripe retries.
func (s *Service) syncSubscription(ctx context.Context, sub *stripe.Subscription) error {
	userID, err := s.userIDForSubscription(ctx, sub)
	if errors.Is(err, errUnknownUser) {
		return nil
	}
	if err != nil {
		return err
	}
	rows := subscriptionToStoreRows(userID, sub)
	keep := make([]string, 0, len(rows))
	for _, row := range rows {
		if err := s.store.UpsertSubscription(ctx, row); err != nil {
			return err
		}
		keep = append(keep, row.PriceID)
	}
	// Prune rows for items detached since the last sync so a plain
	// customer.subscription.updated event converges the projection.
	return s.store.DeleteSubscriptionRowsExcept(ctx, sub.ID, keep)
}

// userIDForSubscription resolves the owning user: preferring the user_id we
// stamped in subscription metadata, falling back to the customer→user link.
func (s *Service) userIDForSubscription(ctx context.Context, sub *stripe.Subscription) (int64, error) {
	if v := sub.Metadata["user_id"]; v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id, nil
		}
	}
	if sub.Customer != nil && sub.Customer.ID != "" {
		if u, err := s.store.GetUserByStripeCustomer(ctx, sub.Customer.ID); err == nil {
			return u.ID, nil
		}
	}
	return 0, errUnknownUser
}

// subscriptionToStoreRows maps a Stripe subscription to one store row per
// subscription item. price_id and current_period_end live on each item (they
// moved off the subscription object in recent API versions); status, trial and
// cancellation state are shared across every row of the subscription. A
// subscription with no items (shouldn't happen) maps to a single price-less row
// so its status still lands somewhere visible.
func subscriptionToStoreRows(userID int64, sub *stripe.Subscription) []store.Subscription {
	base := store.Subscription{
		UserID:               userID,
		StripeSubscriptionID: sub.ID,
		Status:               string(sub.Status),
		Currency:             string(sub.Currency),
		CancelAtPeriodEnd:    sub.CancelAtPeriodEnd,
	}
	if sub.Customer != nil {
		base.StripeCustomerID = sub.Customer.ID
	}
	if sub.TrialEnd > 0 {
		t := time.Unix(sub.TrialEnd, 0).UTC()
		base.TrialEnd = &t
	}
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return []store.Subscription{base}
	}
	out := make([]store.Subscription, 0, len(sub.Items.Data))
	for _, item := range sub.Items.Data {
		row := base
		if item.Price != nil {
			row.PriceID = item.Price.ID
		}
		if item.CurrentPeriodEnd > 0 {
			t := time.Unix(item.CurrentPeriodEnd, 0).UTC()
			row.CurrentPeriodEnd = &t
		}
		out = append(out, row)
	}
	return out
}
