package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// localeCookieMaxAge is how long a language choice survives in the browser. A
// year, because a language preference is not something a person revisits — and
// a signed-in user's real preference lives in the database anyway; the cookie
// is what carries it across sign-out and onto the login page.
const localeCookieMaxAge = int(365 * 24 * time.Hour / time.Second)

// setLocaleCookie remembers a language choice in the browser. Shared with the
// first-run wizard, so a language gets written the same way wherever it is
// chosen.
//
// HttpOnly: nothing on the client reads this. Unlike the theme, which the
// pre-paint script in <head> must read from localStorage to avoid a flash, the
// language is resolved server-side before a byte of HTML is written.
func (h *Handlers) setLocaleCookie(c *gin.Context, l i18n.Locale) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(i18n.CookieName, string(l), localeCookieMaxAge, "/", "", h.secure, true)
}

// SetLocale records the user's language choice and reloads the page.
//
// The route is public on purpose. Someone who cannot read English needs to
// switch the language on the login page, before there is an account to store
// the choice against — so this writes the cookie unconditionally, and
// additionally persists to the user record when the request happens to carry a
// valid session.
func (h *Handlers) SetLocale(c *gin.Context) {
	l, ok := i18n.Parse(c.PostForm("locale"))
	if !ok {
		c.String(http.StatusBadRequest, "unsupported language")
		return
	}

	h.setLocaleCookie(c, l)

	if raw, err := c.Cookie(SessionCookieName); err == nil && raw != "" {
		if user, err := h.auth.AuthenticateSession(c.Request.Context(), raw); err == nil {
			if err := h.store.UpdateUserLocale(c.Request.Context(), user.ID, l); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	// A language change rewrites every string on the page, so there is no
	// meaningful partial to swap — reload rather than trying to patch fragments.
	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Refresh", "true")
		c.Status(http.StatusNoContent)
		return
	}
	c.Redirect(http.StatusSeeOther, c.DefaultPostForm("return_to", "/"))
}
