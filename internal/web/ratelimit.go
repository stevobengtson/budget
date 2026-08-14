package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/ratelimit"
	"github.com/sbengtson/budget/internal/web/handlers"
	"github.com/sbengtson/budget/internal/web/views"
)

// limiters holds the app's rate limits. They are per-process, which is exactly
// right for a single binary on a single host — see internal/core/ratelimit.
type limiters struct {
	registry *ratelimit.Registry
	loginIP  *ratelimit.Limiter
	signupIP *ratelimit.Limiter
	forgotIP *ratelimit.Limiter
	accountU *ratelimit.Limiter
}

// newLimiters builds the limits applied to the unauthenticated surface.
//
// The numbers are set so that a person who has genuinely forgotten which
// password they used never meets them, while a script does so immediately.
func newLimiters() *limiters {
	r := ratelimit.NewRegistry()
	return &limiters{
		registry: r,
		// Sign-in is the expensive one: every attempt costs an argon2id hash.
		loginIP: r.Add("login_ip", ratelimit.Rule{N: 10, Per: 5 * time.Minute}),
		// Signup is where automated account creation would show up.
		signupIP: r.Add("signup_ip", ratelimit.Rule{N: 5, Per: time.Hour}),
		// Password-reset requests send mail to an address the requester does
		// not have to own, so this limit protects other people's inboxes.
		forgotIP: r.Add("forgot_ip", ratelimit.Rule{N: 5, Per: time.Hour}),
		// Security-screen mutations by a signed-in user.
		accountU: r.Add("account_user", ratelimit.Rule{N: 20, Per: time.Hour}),
	}
}

// rateLimitIP throttles by client IP.
//
// Correct only because the router sets trusted proxies: without that, ClientIP
// reflects an attacker-supplied X-Forwarded-For and every request can present a
// fresh key.
func rateLimitIP(l *ratelimit.Limiter) gin.HandlerFunc {
	return rateLimitBy(l, func(c *gin.Context) string { return c.ClientIP() })
}

// rateLimitUser throttles by authenticated user id. Must be mounted inside
// requireAuth.
func rateLimitUser(l *ratelimit.Limiter) gin.HandlerFunc {
	return rateLimitBy(l, func(c *gin.Context) string {
		return strconv.FormatInt(c.GetInt64("userID"), 10)
	})
}

func rateLimitBy(l *ratelimit.Limiter, key func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, retryAfter := l.Allow(key(c))
		if ok {
			c.Next()
			return
		}
		c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeTooManyRequests(c)
		c.Abort()
	}
}

// requireRecentAuth makes a sensitive action wait until the session has proved
// a factor recently.
//
// It answers a stale session with a real 4xx carrying the re-auth card, which
// the settings forms swap in place via the existing [data-inline-errors]
// contract — so this needs no new client-side mechanism. The window is on the
// session rather than the action, so proving yourself once unlocks the whole
// security screen for a few minutes instead of prompting between every click.
func requireRecentAuth(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetString(handlers.ContextSessionToken)
		if !svc.StepUpRequired(c.Request.Context(), token) {
			c.Next()
			return
		}
		// A fetch() caller cannot render an HTML card. Passkey enrolment is
		// driven by script rather than by an HTMX form, so answering it with
		// the card produced a 403 the script could not parse and swallowed —
		// a console error and nothing on screen.
		if wantsJSON(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
				"code":    "reauth_required",
				"message": i18n.T(c.Request.Context(), "auth.err_reauth_required"),
			}})
			c.Abort()
			return
		}
		handlers.Render(c, http.StatusForbidden, views.ReauthCard(c.Request.URL.Path))
		c.Abort()
	}
}

// wantsJSON reports whether the caller expects a JSON body rather than HTML:
// the mobile API, or a script that asked for it explicitly.
func wantsJSON(c *gin.Context) bool {
	return strings.HasPrefix(c.Request.URL.Path, "/api/") ||
		strings.Contains(c.GetHeader("Accept"), "application/json")
}

// writeTooManyRequests answers a throttled request in the shape its caller
// expects: JSON for the mobile API, an HTML page for a browser.
func writeTooManyRequests(c *gin.Context) {
	if wantsJSON(c) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{
			"code":    "rate_limited",
			"message": i18n.T(c.Request.Context(), "auth.err_rate_limited"),
		}})
		return
	}
	handlers.Render(c, http.StatusTooManyRequests,
		views.MessagePage("auth.msg_rate_limited_title", "auth.msg_rate_limited_body", nil, ""))
}
