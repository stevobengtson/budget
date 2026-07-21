package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SessionCookieName is the cookie holding the opaque session token. Exported so
// the router middleware (package web) reads/clears the same cookie.
const SessionCookieName = "budget_session"

// SetSessionCookie writes the session cookie: HttpOnly, SameSite=Lax, Secure per
// config. maxAgeSeconds mirrors the configured session TTL.
func SetSessionCookie(c *gin.Context, raw string, maxAgeSeconds int, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookieName, raw, maxAgeSeconds, "/", "", secure, true)
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookieName, "", -1, "/", "", false, true)
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
