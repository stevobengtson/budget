package handlers

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/web/views"
)

// maxWebhookBytes caps the webhook request body before signature verification.
const maxWebhookBytes = 1 << 20 // 1 MiB

// BillingPage renders the billing/subscription status page. It doubles as the
// landing page the subscription gate redirects lapsed users to.
func (h *Handlers) BillingPage(c *gin.Context) {
	d := h.billingData(c)
	// Managing a plan now lives in Settings. Only a locked-out user — never
	// subscribed, or lapsed — still gets the standalone paywall here, because
	// that is where the subscription gate sends them and they cannot reach
	// Settings at all.
	if !d.Standalone {
		c.Redirect(http.StatusSeeOther, "/account?tab=billing")
		return
	}
	render(c, http.StatusOK, views.BillingPage(d))
}

// billingData builds the billing view model from the user's current subscription.
func (h *Handlers) billingData(c *gin.Context) views.BillingData {
	d := views.BillingData{
		Collapsed:  sidebarCollapsed(c),
		Configured: h.billing.Enabled(),
		State:      "none",
	}
	if !h.billing.Enabled() {
		return d
	}
	uid := currentUserID(c)
	if exempt, _ := h.store.IsBillingExempt(c.Request.Context(), uid); exempt {
		d.State = "exempt" // complimentary account — no subscription needed
		return d
	}
	sub, err := h.store.GetSubscriptionForUser(c.Request.Context(), uid, h.billing.BasePriceID())
	switch {
	case errors.Is(err, sql.ErrNoRows), err != nil:
		// Never subscribed (or couldn't determine) — offer the free trial.
		d.State = "none"
	default:
		d.CancelAtPeriodEnd = sub.CancelAtPeriodEnd
		switch sub.Status {
		case "trialing":
			d.State = "trialing"
			if sub.TrialEnd != nil {
				d.TrialEnd = sub.TrialEnd.Format("January 2, 2006")
				d.TrialDaysLeft = daysUntil(*sub.TrialEnd)
			}
		case "active":
			d.State = "active"
			if sub.CurrentPeriodEnd != nil {
				d.RenewalDate = sub.CurrentPeriodEnd.Format("January 2, 2006")
			}
		case "past_due":
			d.State = "past_due"
		default:
			// canceled / unpaid / incomplete_expired / paused — trial used, must pay.
			d.State = "ended"
		}
	}
	// Locked-out states (no access yet / lapsed) render as a standalone paywall
	// without the app shell; states that grant access render inside the app for
	// in-place management.
	d.Standalone = d.State == "none" || d.State == "ended"
	return d
}

// daysUntil returns whole days from now until t, floored at 0.
func daysUntil(t time.Time) int {
	d := int(time.Until(t).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// StartCheckout creates a Stripe Checkout Session and redirects the user to it.
func (h *Handlers) StartCheckout(c *gin.Context) {
	uid := currentUserID(c)
	u, err := h.store.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/billing")
		return
	}
	url, err := h.billing.StartCheckoutURL(c.Request.Context(), uid, u.Email, u.Name)
	if err != nil {
		_ = c.Error(err)
		c.Redirect(http.StatusSeeOther, "/billing")
		return
	}
	c.Redirect(http.StatusSeeOther, url)
}

// OpenPortal redirects the user to the Stripe Customer Portal to manage their
// subscription (add a card, change plan, cancel).
func (h *Handlers) OpenPortal(c *gin.Context) {
	url, err := h.billing.PortalURL(c.Request.Context(), currentUserID(c))
	if err != nil {
		_ = c.Error(err)
		c.Redirect(http.StatusSeeOther, "/billing")
		return
	}
	c.Redirect(http.StatusSeeOther, url)
}

// CheckoutSuccess is the Checkout success redirect target. It syncs the new
// subscription synchronously (so access is granted without waiting for the
// webhook), then lands the user in the app.
func (h *Handlers) CheckoutSuccess(c *gin.Context) {
	if sessionID := c.Query("session_id"); sessionID != "" {
		if err := h.billing.SyncCheckoutSession(c.Request.Context(), sessionID); err != nil {
			_ = c.Error(err)
		}
	}
	c.Redirect(http.StatusSeeOther, "/budget")
}

// StripeWebhook receives Stripe webhook events. The Stripe signature (verified
// inside billing.HandleWebhook using the signing secret) authenticates the
// request — there's no session. A bad signature or parse error is a 400; a
// handled or ignored event is 200.
func (h *Handlers) StripeWebhook(c *gin.Context) {
	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBytes))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if err := h.billing.HandleWebhook(payload, c.GetHeader("Stripe-Signature")); err != nil {
		_ = c.Error(err)
		c.Status(http.StatusBadRequest)
		return
	}
	c.Status(http.StatusOK)
}
