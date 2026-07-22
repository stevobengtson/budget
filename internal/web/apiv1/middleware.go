package apiv1

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// contextUserID is the gin-context key holding the authenticated user's id.
const contextUserID = "userID"

// RequireBearerAuth authenticates requests via `Authorization: Bearer <token>`,
// reusing the very same opaque session token the web cookie carries — a token is
// a token regardless of transport. On success it stashes the user id/email on
// the context; on failure it aborts with a 401 JSON envelope.
func (a *API) RequireBearerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c)
		if raw == "" {
			writeError(c, http.StatusUnauthorized, "unauthorized", "Missing bearer token.")
			return
		}
		user, err := a.auth.AuthenticateSession(c.Request.Context(), raw)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "Invalid or expired token.")
			return
		}
		c.Set(contextUserID, user.ID)
		c.Set("userEmail", user.Email)
		c.Next()
	}
}

// bearerToken extracts the raw token from the Authorization header, or "" when
// absent/malformed. The scheme match is case-insensitive per RFC 7235.
func bearerToken(c *gin.Context) string {
	const prefix = "Bearer "
	h := c.GetHeader("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
