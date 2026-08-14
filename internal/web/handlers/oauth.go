package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// OAuthStart sends the browser to the provider.
func (h *Handlers) OAuthStart(c *gin.Context) {
	url, err := h.auth.BeginOAuth(c.Request.Context(), c.Param("provider"), 0, store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web",
	})
	if err != nil {
		render(c, http.StatusBadRequest, views.LoginPage("", "auth.err_oauth_unavailable"))
		return
	}
	c.Redirect(http.StatusSeeOther, url)
}

// OAuthLinkStart begins a handshake that attaches a provider to the signed-in
// account rather than signing anybody in.
func (h *Handlers) OAuthLinkStart(c *gin.Context) {
	url, err := h.auth.BeginOAuth(c.Request.Context(), c.Param("provider"), currentUserID(c),
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web"})
	if err != nil {
		h.renderIdentities(c, http.StatusBadRequest,
			i18n.T(c.Request.Context(), "settings.err_identity_generic"))
		return
	}
	// A full redirect, not an HTMX swap: the browser is leaving for the
	// provider, which HTMX cannot follow.
	c.Header("HX-Redirect", url)
	c.Redirect(http.StatusSeeOther, url)
}

// OAuthCallback completes a handshake.
//
// Registered for both GET and POST. Apple returns a cross-site form_post when
// any scope is requested, and it will not release the email otherwise, so the
// POST shape is not optional.
func (h *Handlers) OAuthCallback(c *gin.Context) {
	state, code := c.Query("state"), c.Query("code")
	if c.Request.Method == http.MethodPost {
		state, code = c.PostForm("state"), c.PostForm("code")
	}
	// The provider reports a refusal here rather than by failing the exchange;
	// a user who pressed Cancel is not an error worth alarming them about.
	if c.Query("error") != "" || c.PostForm("error") != "" {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}

	res, err := h.auth.FinishOAuth(c.Request.Context(), state, code, store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web",
	})
	if err != nil {
		render(c, http.StatusUnauthorized, views.LoginPage("", oauthErrKey(err)))
		return
	}
	if res.Linked {
		c.Redirect(http.StatusSeeOther, "/account?tab=security")
		return
	}
	// Federated sign-in is a first factor, so an account with 2FA still gets a
	// challenge.
	if res.Login.NeedsChallenge() {
		render(c, http.StatusOK, views.ChallengePage(views.ChallengeData{
			Token:       res.Login.ChallengeToken,
			Method:      res.Login.Methods[0],
			Methods:     res.Login.Methods,
			MaskedEmail: res.Login.MaskedEmail,
		}))
		return
	}
	SetSessionCookie(c, res.Login.SessionToken, h.sessionMaxAge, h.secure)
	c.Redirect(http.StatusSeeOther, "/budget")
}

// oauthErrKey maps a handshake failure to something the user can act on.
func oauthErrKey(err error) string {
	switch {
	case errors.Is(err, auth.ErrIdentityUnverified):
		return "auth.err_oauth_unverified"
	case errors.Is(err, auth.ErrNoAccountForIdentity):
		return "auth.err_oauth_no_account"
	case errors.Is(err, auth.ErrIdentityTaken):
		return "auth.err_oauth_taken"
	case errors.Is(err, auth.ErrAccountDisabled):
		return "auth.err_disabled"
	default:
		return "auth.err_oauth_failed"
	}
}

// AccountIdentities renders the linked-accounts card on its own.
func (h *Handlers) AccountIdentities(c *gin.Context) {
	render(c, http.StatusOK, views.AccountIdentitiesCard(h.identitiesData(c)))
}

// UnlinkIdentity detaches a provider.
func (h *Handlers) UnlinkIdentity(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		h.renderIdentities(c, http.StatusBadRequest, i18n.T(ctx, "settings.err_identity_generic"))
		return
	}
	switch err := h.auth.UnlinkIdentity(ctx, currentUserID(c), id); {
	case err == nil:
	case errors.Is(err, auth.ErrLastFactor):
		// Removing it would leave nothing that can sign in.
		h.renderIdentities(c, http.StatusBadRequest, i18n.T(ctx, "settings.err_identity_last"))
		return
	default:
		h.renderIdentities(c, http.StatusBadRequest, i18n.T(ctx, "settings.err_identity_generic"))
		return
	}
	d := h.identitiesData(c)
	d.IdentityOK = i18n.T(ctx, "settings.ok_identity_unlinked")
	render(c, http.StatusOK, views.AccountIdentitiesCard(d))
}

func (h *Handlers) renderIdentities(c *gin.Context, status int, errMsg string) {
	d := h.identitiesData(c)
	d.IdentityErr = errMsg
	render(c, status, views.AccountIdentitiesCard(d))
}

// identitiesData builds the linked-accounts view model.
func (h *Handlers) identitiesData(c *gin.Context) views.IdentitiesData {
	uid := currentUserID(c)
	ids, err := h.auth.ListIdentities(c.Request.Context(), uid)
	if err != nil {
		_ = c.Error(err)
	}
	linked := map[string]bool{}
	out := views.IdentitiesData{Providers: h.auth.OAuthProviders()}
	for _, id := range ids {
		linked[id.Provider] = true
		out.Linked = append(out.Linked, views.IdentityView{
			ID:       id.ID,
			Provider: id.Provider,
			Email:    id.Email,
			Added:    id.CreatedAt,
		})
	}
	for _, p := range out.Providers {
		if !linked[p] {
			out.Unlinked = append(out.Unlinked, p)
		}
	}
	return out
}
