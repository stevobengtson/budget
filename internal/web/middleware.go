package web

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
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
		// Also stash the name + email on the request context so the shared layout
		// can render the sidebar user menu without every page threading it through.
		ctx := views.WithUserEmail(c.Request.Context(), user.Email)
		ctx = views.WithUserName(ctx, user.Name)
		ctx = views.WithUserAvatarVersion(ctx, user.AvatarVersion())
		// Enabled add-ons gate add-on-owned nav (e.g. Paydown) in the shared layout
		// and the requireAddOn route guard.
		slugs, _ := st.EnabledAddOnSlugs(c.Request.Context(), user.ID)
		ctx = views.WithEnabledAddOns(ctx, slugs)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
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
