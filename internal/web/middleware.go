package web

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
	"github.com/sbengtson/budget/internal/web/views"
)

// requestLogger logs one structured line per request via slog (→ journald).
// It is registered as the outermost middleware so its deferred log records the
// final status code, even after gin.Recovery() turns a panic into a 500.
//
// The URL path is logged WITHOUT the query string on purpose: auth links carry
// verification/reset tokens in the query, which must never land in logs.
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// Skip high-volume, low-signal paths so real requests stay readable.
		if strings.HasPrefix(path, "/static") ||
			strings.HasPrefix(path, "/templui/") ||
			path == "/healthz" || path == "/favicon.ico" {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"dur_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		}
		// Handlers attach the underlying failure via c.Error; without this the
		// journal shows a bare 5xx with no cause (as the Plaid 503s did).
		if len(c.Errors) > 0 {
			attrs = append(attrs, "err", c.Errors.String())
		}
		switch {
		case status >= 500:
			slog.Error("http request", attrs...)
		case status >= 400:
			slog.Warn("http request", attrs...)
		default:
			slog.Info("http request", attrs...)
		}
	}
}

// resolveLocale puts a locale on every request's context, signed in or not.
//
// It runs globally rather than inside requireAuth because the pages a visitor
// sees before they have an account — login, registration, password reset — are
// exactly the ones where getting the language right matters most: someone who
// cannot read the sign-up form never becomes a user whose preference we could
// have stored. Those pages resolve from the cookie, then Accept-Language.
//
// requireAuth runs after this and overwrites the value with the signed-in
// user's stored preference, which is the authoritative choice.
func resolveLocale() gin.HandlerFunc {
	return func(c *gin.Context) {
		l := i18n.FromRequest(c.Request)
		c.Request = c.Request.WithContext(i18n.WithLocale(c.Request.Context(), l))
		c.Next()
	}
}

// pinLocale forces a locale onto the request, overriding the cookie and
// Accept-Language that resolveLocale worked out.
//
// It backs the public marketing and legal pages, which are served at one URL
// per language (/privacy and /fr/privacy). There the URL is the whole point:
// the pages have to be separately indexable and separately linkable, so a
// visitor's cookie must not change which language a given address returns.
func pinLocale(l i18n.Locale) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(i18n.WithLocale(c.Request.Context(), l))
		c.Next()
	}
}

// requireAuth loads the session user into the context or redirects to /login.
func requireAuth(svc *auth.Service, st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := c.Cookie(handlers.SessionCookieName)
		if err != nil || raw == "" {
			c.Redirect(http.StatusSeeOther, "/login")
			c.Abort()
			return
		}
		user, err := svc.AuthenticateSession(c.Request.Context(), raw)
		if err != nil {
			handlers.ClearSessionCookie(c)
			c.Redirect(http.StatusSeeOther, "/login")
			c.Abort()
			return
		}
		c.Set("userID", user.ID)
		c.Set("userEmail", user.Email)
		c.Set("userIsAdmin", user.IsAdmin)
		c.Set("userOnboarded", user.Onboarded())
		// The stored preference wins over anything resolveLocale worked out from
		// the cookie or the browser: a signed-in user has said what they want.
		c.Request = c.Request.WithContext(i18n.WithLocale(c.Request.Context(), user.Locale))
		// Also stash the name + email on the request context so the shared layout
		// can render the sidebar user menu without every page threading it through.
		ctx := views.WithUserEmail(c.Request.Context(), user.Email)
		ctx = views.WithUserName(ctx, user.Name)
		ctx = views.WithUserAvatarVersion(ctx, user.AvatarVersion())
		// Drives the Admin entry in the shared user menu; the routes themselves
		// are guarded by requireAdmin, which re-reads the flag from the database.
		ctx = views.WithIsAdmin(ctx, user.IsAdmin)
		// Enabled add-ons gate add-on-owned nav (e.g. Paydown) in the shared layout
		// and the requireAddOn route guard.
		slugs, _ := st.EnabledAddOnSlugs(c.Request.Context(), user.ID)
		ctx = views.WithEnabledAddOns(ctx, slugs)
		// The Activity nav badge: unreviewed bank-sync imports. Only counted when
		// the add-on is on (the partial index makes it one cheap lookup).
		if slices.Contains(slugs, "bank_sync") {
			if n, err := st.CountNeedsReview(c.Request.Context(), user.ID); err == nil {
				ctx = views.WithReviewCount(ctx, n)
			}
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// requireOnboarded sends a user who has not finished the first-run wizard to it.
//
// It reads the flag requireAuth loaded on this request, so finishing the wizard
// takes effect on the very next one. It must NOT be applied to /welcome itself,
// or the redirect loops.
//
// The wizard sits behind the subscription gate rather than in front of it,
// because it seeds a budget the user cannot reach without a subscription
// anyway — a trial is started first, then the wizard runs.
func requireOnboarded() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("userOnboarded") {
			c.Next()
			return
		}
		gateRedirect(c, "/welcome")
	}
}

// requireSubscription gates the core app behind an active trial or paid
// subscription. It's a no-op when billing isn't configured, so dev environments
// without Stripe still work.
//
// A user without access is redirected (HX-aware) based on their history:
//   - never subscribed (no row) → /billing/start, which opens the free-trial
//     Checkout automatically (the "no card required" trial).
//   - lapsed/canceled (row, non-granting status) → /billing, the manual page to
//     resubscribe (also the safe Checkout cancel landing).
//
// The gate itself makes no Stripe calls (that happens in the /billing/start
// handler), so it stays fast and side-effect-free.
func requireSubscription(st *store.Store, bill *billing.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !bill.Enabled() {
			c.Next()
			return
		}
		uid := c.GetInt64("userID")
		sub, err := st.GetSubscriptionForUser(c.Request.Context(), uid, bill.BasePriceID())
		if err == nil && store.AccessGranting(sub.Status) {
			c.Next()
			return
		}
		// Complimentary / always-free accounts (set manually in the DB) bypass the
		// gate. Checked only when there's no active subscription, so paying users
		// don't incur the extra lookup.
		if exempt, _ := st.IsBillingExempt(c.Request.Context(), uid); exempt {
			c.Next()
			return
		}
		dest := "/billing"
		if errors.Is(err, sql.ErrNoRows) {
			dest = "/billing/start"
		}
		gateRedirect(c, dest)
	}
}

// gateRedirect aborts the request with a redirect to dest, using an HX-Redirect
// header for HTMX requests (so a partial swap navigates) and a 303 otherwise.
func gateRedirect(c *gin.Context, dest string) {
	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Redirect", dest)
		c.Status(http.StatusNoContent)
	} else {
		c.Redirect(http.StatusSeeOther, dest)
	}
	c.Abort()
}

// requireAdmin guards the /admin console. requireAuth must run first; this reads
// the is_admin flag it loaded from the database on this request, so revoking the
// flag takes effect on the very next request rather than at session expiry.
//
// A non-admin gets a plain 404, not a 403: the console's existence is not
// something a regular user needs confirmed, and 403 would confirm it.
func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool("userIsAdmin") {
			c.Next()
			return
		}
		c.Status(http.StatusNotFound)
		c.Abort()
	}
}

// requireAddOn guards routes owned by an opt-in add-on. It reads the enabled
// add-ons stashed on the context by requireAuth (which must run first) and, when
// the add-on is off, sends the user back to /budget instead of the feature. HTMX
// requests get an HX-Redirect so partial swaps navigate rather than inline the
// redirect target.
func requireAddOn(slug string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if views.AddOnEnabled(c.Request.Context(), slug) {
			c.Next()
			return
		}
		if c.GetHeader("HX-Request") == "true" {
			c.Header("HX-Redirect", "/budget")
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Redirect(http.StatusSeeOther, "/budget")
		c.Abort()
	}
}
