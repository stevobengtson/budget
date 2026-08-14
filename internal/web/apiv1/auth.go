package apiv1

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
)

// loginRequest is the JSON body the app POSTs to /login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Client identifies the app for the account's active-sessions list
	// ("ios", "android"). Optional: builds already in the stores do not send
	// it, and those sessions simply show as a generic mobile client.
	Client string `json:"client"`
}

// userPayload is the public shape of a user in API responses (never the hash).
type userPayload struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// authResponse is returned by /login: the bearer token for the app to persist
// in the Keychain (iOS) / Keystore (Android), plus enough profile + gate state
// to route straight to the budget or the paywall without a second round-trip.
// AddOns are the user's enabled add-on slugs (e.g. "paydown"), which drive
// optional nav in the apps.
type authResponse struct {
	Token      string      `json:"token"`
	User       userPayload `json:"user"`
	Subscribed bool        `json:"subscribed"`
	AddOns     []string    `json:"addOns"`
}

// Login validates credentials and issues a session token. It reuses the exact
// web login flow (auth.Service.Login), so a token minted here is identical to a
// cookie session and works with the same middleware.
func (a *API) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with email and password.")
		return
	}
	client := req.Client
	if client != "ios" && client != "android" {
		client = "mobile"
	}
	raw, err := a.auth.Login(c.Request.Context(), req.Email, req.Password, store.SessionInfo{
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
		Client:    client,
	})
	if err != nil {
		// Surfaced as 429 with Retry-After so the app can say how long, rather
		// than looking like a rejected password the user could retype.
		if wait, locked := auth.LockedOut(err); locked {
			c.Header("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(c, http.StatusTooManyRequests, "locked_out",
				i18n.Tnf(c.Request.Context(), "auth.err_locked_out", int(wait.Minutes())+1,
					i18n.M{"Minutes": int(wait.Minutes()) + 1}))
			return
		}
		if errors.Is(err, auth.ErrEmailNotVerified) {
			writeError(c, http.StatusUnauthorized, "email_not_verified", "Please verify your email before logging in.")
			return
		}
		if errors.Is(err, auth.ErrAccountDisabled) {
			writeError(c, http.StatusForbidden, "account_disabled", "This account has been disabled.")
			return
		}
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}
	user, err := a.auth.AuthenticateSession(c.Request.Context(), raw)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not establish the session.")
		return
	}
	c.JSON(http.StatusOK, authResponse{
		Token:      raw,
		User:       userPayload{ID: user.ID, Email: user.Email, Name: user.Name},
		Subscribed: a.subscribed(c.Request.Context(), user.ID),
		AddOns:     a.enabledAddOns(c.Request.Context(), user.ID),
	})
}

// Logout revokes the presented session token. Idempotent: an already-invalid
// token still returns 204.
func (a *API) Logout(c *gin.Context) {
	if raw := bearerToken(c); raw != "" {
		_ = a.auth.Logout(c.Request.Context(), raw)
	}
	c.Status(http.StatusNoContent)
}

// meResponse is returned by /me — the current user plus their access state, so
// the app can re-check the paywall on launch with the token it already holds.
type meResponse struct {
	User       userPayload `json:"user"`
	Subscribed bool        `json:"subscribed"`
	AddOns     []string    `json:"addOns"`
}

// Me returns the authenticated user. RequireBearerAuth has already validated the
// token and stashed the id on the context.
func (a *API) Me(c *gin.Context) {
	uid := c.GetInt64(contextUserID)
	user, err := a.store.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load your account.")
		return
	}
	c.JSON(http.StatusOK, meResponse{
		User:       userPayload{ID: user.ID, Email: user.Email, Name: user.Name},
		Subscribed: a.subscribed(c.Request.Context(), uid),
		AddOns:     a.enabledAddOns(c.Request.Context(), uid),
	})
}

// enabledAddOns returns the user's enabled add-on slugs, always non-nil so the
// JSON is [] rather than null (simpler for the mobile clients to decode).
func (a *API) enabledAddOns(ctx context.Context, userID int64) []string {
	slugs, err := a.store.EnabledAddOnSlugs(ctx, userID)
	if err != nil || slugs == nil {
		return []string{}
	}
	return slugs
}

// subscribed reports whether the user currently has app access — the mobile
// mirror of the web's requireSubscription gate (internal/web/middleware.go).
// The policy, in order:
//
//  1. Billing not configured (dev / self-host, no Stripe): open to everyone,
//     matching the web so the API isn't stricter than the site.
//  2. A subscription in an access-granting status (trialing / active / past_due,
//     the dunning grace window) grants access.
//  3. Otherwise, complimentary accounts flagged billing_exempt get in.
//
// The exempt lookup runs only when there's no granting subscription, so paying
// users never incur the extra query — same trade-off the web gate makes.
func (a *API) subscribed(ctx context.Context, userID int64) bool {
	if !a.billing.Enabled() {
		return true
	}
	sub, err := a.store.GetSubscriptionForUser(ctx, userID, a.billing.BasePriceID())
	if err == nil && store.AccessGranting(sub.Status) {
		return true
	}
	exempt, _ := a.store.IsBillingExempt(ctx, userID)
	return exempt
}
