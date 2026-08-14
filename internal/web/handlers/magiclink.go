package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// RequestMagicLink emails a sign-in link.
//
// The response is identical whether or not the address has an account: telling
// the two apart would make this a way to test which addresses are registered.
func (h *Handlers) RequestMagicLink(c *gin.Context) {
	email := c.PostForm("email")
	if err := h.auth.RequestMagicLink(c.Request.Context(), email, store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web",
	}); err != nil {
		_ = c.Error(err)
	}
	render(c, http.StatusOK, views.MessagePage("auth.msg_magic_sent_title", "auth.msg_magic_sent_body",
		nil, "auth.remember_password"))
}

// MagicLinkForm renders the confirmation interstitial the emailed link lands on.
//
// This GET deliberately does NOT sign anyone in. Corporate mail scanners —
// Outlook Safe Links, security proxies, link previewers — fetch every URL in an
// incoming message. A link that authenticated on GET would be consumed by a
// robot before the recipient ever saw it, and in the worst case would hand a
// session to whatever fetched it. The POST behind the button is what completes
// the sign-in.
func (h *Handlers) MagicLinkForm(c *gin.Context) {
	challenge := c.Query("c")
	code := c.Query("k")

	if err := h.auth.LookupMagicLink(c.Request.Context(), challenge); err != nil {
		render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_magic_expired"))
		return
	}
	render(c, http.StatusOK, views.MagicConfirmPage(challenge, code, ""))
}

// ConfirmMagicLink completes a link sign-in.
func (h *Handlers) ConfirmMagicLink(c *gin.Context) {
	challenge := c.PostForm("challenge")
	code := c.PostForm("code")

	r, err := h.auth.ConfirmMagicLink(c.Request.Context(), challenge, code, store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web",
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_too_many_attempts"))
		case errors.Is(err, auth.ErrInvalidCredentials):
			// Wrong code typed on the interstitial; the link itself is still live.
			render(c, http.StatusUnauthorized, views.MagicConfirmPage(challenge, "", "auth.err_code_invalid"))
		default:
			render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_magic_expired"))
		}
		return
	}
	// A magic link is a first factor, so an account with two-step verification
	// still has to clear it.
	if r.NeedsChallenge() {
		render(c, http.StatusOK, views.ChallengePage(views.ChallengeData{
			Token:       r.ChallengeToken,
			Method:      r.Methods[0],
			Methods:     r.Methods,
			MaskedEmail: r.MaskedEmail,
		}))
		return
	}
	SetSessionCookie(c, r.SessionToken, h.sessionMaxAge, h.secure)
	c.Redirect(http.StatusSeeOther, "/budget")
}
