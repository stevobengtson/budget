package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/web/views"
)

// AccountSessions renders the active-sessions card on its own, for the HTMX
// swap after a revoke.
func (h *Handlers) AccountSessions(c *gin.Context) {
	render(c, http.StatusOK, views.AccountSessionsCard(h.sessionsData(c)))
}

// RevokeSession signs one other device out.
func (h *Handlers) RevokeSession(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		d := h.sessionsData(c)
		d.SessionsErr = i18n.T(c.Request.Context(), "settings.err_session_revoke")
		render(c, http.StatusBadRequest, views.AccountSessionsCard(d))
		return
	}
	uid := currentUserID(c)
	// The store scopes the delete by user_id, so a forged id belonging to
	// someone else deletes nothing and lands here as "not found" — the check and
	// the action are the same statement, which is why there is no ownership
	// check to forget.
	if err := h.auth.RevokeSession(c.Request.Context(), uid, id); err != nil {
		d := h.sessionsData(c)
		d.SessionsErr = i18n.T(c.Request.Context(), "settings.err_session_revoke")
		render(c, http.StatusBadRequest, views.AccountSessionsCard(d))
		return
	}
	d := h.sessionsData(c)
	d.SessionsOK = i18n.T(c.Request.Context(), "settings.ok_session_revoked")
	render(c, http.StatusOK, views.AccountSessionsCard(d))
}

// RevokeOtherSessions signs out every device except this one — including the
// user's phone, whose bearer token is a row in the same table.
func (h *Handlers) RevokeOtherSessions(c *gin.Context) {
	uid := currentUserID(c)
	n, err := h.auth.RevokeOtherSessions(c.Request.Context(), uid, currentSessionToken(c))
	d := h.sessionsData(c)
	if err != nil {
		_ = c.Error(err)
		d.SessionsErr = i18n.T(c.Request.Context(), "settings.err_session_revoke")
		render(c, http.StatusInternalServerError, views.AccountSessionsCard(d))
		return
	}
	d.SessionsOK = i18n.Tnf(c.Request.Context(), "settings.ok_sessions_revoked", n, i18n.M{"Count": n})
	render(c, http.StatusOK, views.AccountSessionsCard(d))
}

// StepUp re-proves a factor, opening the window for the sensitive actions on
// the security screen. It deliberately sits outside requireRecentAuth: proving
// yourself cannot require having already proved yourself.
func (h *Handlers) StepUp(c *gin.Context) {
	uid := currentUserID(c)
	next := c.PostForm("next")
	err := h.auth.StepUpWithPassword(c.Request.Context(), uid, currentSessionToken(c), c.PostForm("password"))
	if err != nil {
		render(c, http.StatusUnauthorized, views.ReauthCardWithError(next, "auth.err_wrong_password"))
		return
	}
	// The window is open now, so send the browser back to redo whatever it was
	// blocked from. HX-Refresh re-runs the current page rather than guessing a
	// target, which keeps this one handler usable from every gated form.
	c.Header("HX-Refresh", "true")
	c.Status(http.StatusNoContent)
}

// sessionsData builds the sessions card's view model.
func (h *Handlers) sessionsData(c *gin.Context) views.SessionsData {
	uid := currentUserID(c)
	list, err := h.auth.ListSessions(c.Request.Context(), uid, currentSessionToken(c))
	if err != nil {
		_ = c.Error(err)
	}
	return views.SessionsData{Sessions: list}
}
