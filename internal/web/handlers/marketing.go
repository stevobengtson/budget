package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/web/views"
)

// Home serves the public marketing landing page. A visitor with a valid session
// is sent straight into the app; anyone else — logged out, or holding a stale or
// invalid session cookie — sees the landing page. We validate the cookie (rather
// than just check for its presence) so a bad cookie doesn't bounce the visitor
// into the auth flow instead of the marketing page.
func (h *Handlers) Home(c *gin.Context) {
	if raw, err := c.Cookie(SessionCookieName); err == nil && raw != "" {
		if _, err := h.auth.AuthenticateSession(c.Request.Context(), raw); err == nil {
			c.Redirect(http.StatusSeeOther, "/budget")
			return
		}
	}
	render(c, http.StatusOK, views.LandingPage())
}
