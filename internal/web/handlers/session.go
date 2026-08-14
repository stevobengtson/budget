package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SessionCookieName is the cookie holding the opaque session token. Exported so
// the router middleware (package web) reads/clears the same cookie.
const SessionCookieName = "budget_session"

// ContextSessionToken is the gin context key under which requireAuth stashes
// the caller's raw session token, for handlers that act on the session itself
// rather than on its user.
const ContextSessionToken = "sessionToken"

// currentSessionToken returns the raw token requireAuth stashed, or "".
func currentSessionToken(c *gin.Context) string { return c.GetString(ContextSessionToken) }

// SetSessionCookie writes the session cookie: HttpOnly, SameSite=Lax, Secure per
// config. maxAgeSeconds mirrors the configured session TTL.
func SetSessionCookie(c *gin.Context, raw string, maxAgeSeconds int, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookieName, raw, maxAgeSeconds, "/", "", secure, true)
}

// ClearSessionCookie removes the session cookie.
//
// secure must match whatever SetSessionCookie used: browsers treat the Secure
// attribute as part of a cookie's identity, so clearing with the wrong value
// can leave the original cookie in place.
func ClearSessionCookie(c *gin.Context, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookieName, "", -1, "/", "", secure, true)
}

// currentUserID returns the authenticated user id set by the requireAuth
// middleware, or 0 when unauthenticated.
func currentUserID(c *gin.Context) int64 {
	if v, ok := c.Get("userID"); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}
