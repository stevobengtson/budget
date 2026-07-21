package web

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/web/handlers"
	"github.com/sbengtson/budget/internal/web/views"
)

// requireAuth loads the session user into the context or redirects to /login.
func requireAuth(svc *auth.Service) gin.HandlerFunc {
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
		// Also stash the email on the request context so the shared layout can
		// render the sidebar user menu without every page threading it through.
		c.Request = c.Request.WithContext(views.WithUserEmail(c.Request.Context(), user.Email))
		c.Next()
	}
}
