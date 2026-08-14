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
	// MFA opts the client in to the two-step sign-in contract.
	//
	// Builds already shipped to TestFlight and Play decode a flat
	// {token,user,...} and have no concept of a challenge. They do not send
	// this, so they get a 401 whose message tells the user to update, rather
	// than a response shape they would fail to parse or, worse, misread as a
	// successful sign-in.
	MFA bool `json:"mfa"`
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
	// Status discriminates the two outcomes for MFA-aware clients: "ok" or
	// "mfa_required". Additive — the fields an older client reads stay exactly
	// where they were on the success path.
	Status     string      `json:"status,omitempty"`
	Token      string      `json:"token"`
	User       userPayload `json:"user"`
	Subscribed bool        `json:"subscribed"`
	AddOns     []string    `json:"addOns"`
}

// challengeResponse is returned to an MFA-aware client when a second factor is
// outstanding. The challenge token is the API's equivalent of the hidden form
// field the web flow uses — same value, same lifetime, same service call.
type challengeResponse struct {
	Status      string   `json:"status"`
	Challenge   string   `json:"challenge"`
	Methods     []string `json:"methods"`
	MaskedEmail string   `json:"maskedEmail"`
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
	r, err := a.auth.BeginLogin(c.Request.Context(), req.Email, req.Password, store.SessionInfo{
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

	if r.NeedsChallenge() {
		if !req.MFA {
			// An older build cannot finish this. Abandon the challenge so it
			// does not sit open, and return the remedy as the message — both
			// clients already surface error.message verbatim.
			_ = a.auth.AbandonChallenge(c.Request.Context(), r.ChallengeToken)
			writeError(c, http.StatusUnauthorized, "mfa_required",
				i18n.T(c.Request.Context(), "auth.err_mfa_required_update_app"))
			return
		}
		c.JSON(http.StatusOK, challengeResponse{
			Status:      "mfa_required",
			Challenge:   r.ChallengeToken,
			Methods:     factorStrings(r.Methods),
			MaskedEmail: r.MaskedEmail,
		})
		return
	}

	a.writeSession(c, r.SessionToken)
}

// Challenge completes a second-factor sign-in for the mobile apps.
func (a *API) Challenge(c *gin.Context) {
	var req struct {
		Challenge string `json:"challenge"`
		Method    string `json:"method"`
		Code      string `json:"code"`
		Client    string `json:"client"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with challenge, method and code.")
		return
	}
	client := req.Client
	if client != "ios" && client != "android" {
		client = "mobile"
	}
	raw, err := a.auth.CompleteChallenge(c.Request.Context(), req.Challenge, auth.Factor(req.Method), req.Code,
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: client})
	if err != nil {
		ctx := c.Request.Context()
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			writeError(c, http.StatusUnauthorized, "too_many_attempts", i18n.T(ctx, "auth.err_too_many_attempts"))
		case errors.Is(err, auth.ErrChallengeExpired):
			writeError(c, http.StatusUnauthorized, "challenge_expired", i18n.T(ctx, "auth.err_challenge_expired"))
		default:
			writeError(c, http.StatusUnauthorized, "invalid_code", i18n.T(ctx, "auth.err_code_invalid"))
		}
		return
	}
	a.writeSession(c, raw)
}

// ChallengeSendCode emails a fresh one-time code for an open challenge.
func (a *API) ChallengeSendCode(c *gin.Context) {
	var req struct {
		Challenge string `json:"challenge"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with challenge.")
		return
	}
	if err := a.auth.SendChallengeCode(c.Request.Context(), req.Challenge); err != nil {
		writeError(c, http.StatusUnauthorized, "challenge_expired",
			i18n.T(c.Request.Context(), "auth.err_challenge_expired"))
		return
	}
	c.Status(http.StatusNoContent)
}

// MagicLinkRequest emails a sign-in link.
//
// Always 204, whether or not the address has an account: distinguishing the two
// would make this a way to test which addresses are registered.
func (a *API) MagicLinkRequest(c *gin.Context) {
	var req struct {
		Email  string `json:"email"`
		Client string `json:"client"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with email.")
		return
	}
	client := req.Client
	if client != "ios" && client != "android" {
		client = "mobile"
	}
	if err := a.auth.RequestMagicLink(c.Request.Context(), req.Email, store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: client,
	}); err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not send the sign-in link.")
		return
	}
	c.Status(http.StatusNoContent)
}

// MagicLinkConfirm completes a link sign-in.
//
// The app receives the link as a universal/app link — the applinks half of the
// association documents — and hands the two query parameters back here.
func (a *API) MagicLinkConfirm(c *gin.Context) {
	var req struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
		Client    string `json:"client"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Expected a JSON body with challenge and code.")
		return
	}
	client := req.Client
	if client != "ios" && client != "android" {
		client = "mobile"
	}
	r, err := a.auth.ConfirmMagicLink(c.Request.Context(), req.Challenge, req.Code,
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: client})
	if err != nil {
		ctx := c.Request.Context()
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			writeError(c, http.StatusUnauthorized, "too_many_attempts", i18n.T(ctx, "auth.err_too_many_attempts"))
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(c, http.StatusUnauthorized, "invalid_code", i18n.T(ctx, "auth.err_code_invalid"))
		default:
			writeError(c, http.StatusUnauthorized, "link_expired", i18n.T(ctx, "auth.err_magic_expired"))
		}
		return
	}
	// A magic link is a first factor, so an account with 2FA still gets a
	// challenge — the same shape every other sign-in path returns.
	if r.NeedsChallenge() {
		c.JSON(http.StatusOK, challengeResponse{
			Status:      "mfa_required",
			Challenge:   r.ChallengeToken,
			Methods:     factorStrings(r.Methods),
			MaskedEmail: r.MaskedEmail,
		})
		return
	}
	a.writeSession(c, r.SessionToken)
}

// Security reports the account's two-factor state for the app's settings screen.
func (a *API) Security(c *gin.Context) {
	st, err := a.auth.SecurityOverview(c.Request.Context(), c.GetInt64(contextUserID))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load your security settings.")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"totpEnabled":       st.TOTPEnabled,
		"emailOtpEnabled":   st.EmailOTPEnabled,
		"recoveryRemaining": st.RecoveryRemaining,
		"hasPassword":       st.HasPassword,
	})
}

// writeSession renders the successful sign-in payload.
func (a *API) writeSession(c *gin.Context, raw string) {
	user, err := a.auth.AuthenticateSession(c.Request.Context(), raw)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not establish the session.")
		return
	}
	c.JSON(http.StatusOK, authResponse{
		Status:     "ok",
		Token:      raw,
		User:       userPayload{ID: user.ID, Email: user.Email, Name: user.Name},
		Subscribed: a.subscribed(c.Request.Context(), user.ID),
		AddOns:     a.enabledAddOns(c.Request.Context(), user.ID),
	})
}

// factorStrings renders factor identifiers for JSON.
func factorStrings(fs []auth.Factor) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f)
	}
	return out
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
