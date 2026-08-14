package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// maxCeremonyBytes caps an authenticator response before it is parsed. These
// are a few kilobytes in practice; the limit stops an unauthenticated endpoint
// being handed an arbitrarily large body.
const maxCeremonyBytes = 64 << 10

// ceremonyCookie carries the WebAuthn ceremony token between the two round
// trips of a sign-in.
//
// A cookie here, unlike the MFA challenge's hidden form field, because the
// exchange is driven by JavaScript with no form to put a field in. HttpOnly so
// the script cannot read it, SameSite=Strict because a passkey ceremony is
// never legitimately started by another site.
const ceremonyCookie = "budget_webauthn"

// PasskeyLoginBegin starts a discoverable sign-in.
func (h *Handlers) PasskeyLoginBegin(c *gin.Context) {
	token, options, err := h.auth.BeginPasskeyLogin(c.Request.Context(), store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web",
	})
	if err != nil {
		writePasskeyError(c, http.StatusServiceUnavailable, "unavailable", err)
		return
	}
	h.setCeremonyCookie(c, token)
	c.Data(http.StatusOK, "application/json; charset=utf-8", options)
}

// PasskeyLoginFinish validates the assertion and signs the user in.
func (h *Handlers) PasskeyLoginFinish(c *gin.Context) {
	token, err := c.Cookie(ceremonyCookie)
	if err != nil || token == "" {
		writePasskeyError(c, http.StatusBadRequest, "expired", auth.ErrChallengeExpired)
		return
	}
	h.clearCeremonyCookie(c)

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCeremonyBytes))
	if err != nil {
		writePasskeyError(c, http.StatusBadRequest, "invalid", err)
		return
	}
	r, err := h.auth.FinishPasskeyLogin(c.Request.Context(), token, body, store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web",
	})
	if err != nil {
		writePasskeyError(c, http.StatusUnauthorized, "rejected", err)
		return
	}
	// An assertion the authenticator did not verify proves possession only, so
	// the account's second factor still applies. The browser is sent to the
	// challenge page rather than signed in.
	if r.NeedsChallenge() {
		c.JSON(http.StatusOK, gin.H{"status": "mfa_required", "challenge": r.ChallengeToken})
		return
	}
	SetSessionCookie(c, r.SessionToken, h.sessionMaxAge, h.secure)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "redirect": "/budget"})
}

// PasskeyRegisterBegin starts enrolling a passkey for the signed-in user.
func (h *Handlers) PasskeyRegisterBegin(c *gin.Context) {
	token, options, err := h.auth.BeginPasskeyRegistration(c.Request.Context(), currentUserID(c),
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web"})
	if err != nil {
		writePasskeyError(c, http.StatusServiceUnavailable, "unavailable", err)
		return
	}
	h.setCeremonyCookie(c, token)
	c.Data(http.StatusOK, "application/json; charset=utf-8", options)
}

// PasskeyRegisterFinish stores the new credential and re-renders the card.
func (h *Handlers) PasskeyRegisterFinish(c *gin.Context) {
	token, err := c.Cookie(ceremonyCookie)
	if err != nil || token == "" {
		writePasskeyError(c, http.StatusBadRequest, "expired", auth.ErrChallengeExpired)
		return
	}
	h.clearCeremonyCookie(c)

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCeremonyBytes))
	if err != nil {
		writePasskeyError(c, http.StatusBadRequest, "invalid", err)
		return
	}
	if err := h.auth.FinishPasskeyRegistration(c.Request.Context(), currentUserID(c), token, body, ""); err != nil {
		writePasskeyError(c, http.StatusBadRequest, "rejected", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// AccountPasskeys renders the passkey card on its own, for the HTMX swap after
// a change.
func (h *Handlers) AccountPasskeys(c *gin.Context) {
	render(c, http.StatusOK, views.AccountPasskeysCard(h.passkeyData(c)))
}

// RenamePasskey relabels a credential.
func (h *Handlers) RenamePasskey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		h.renderPasskeys(c, http.StatusBadRequest, i18n.T(c.Request.Context(), "settings.err_passkey_generic"))
		return
	}
	name := c.PostForm("name")
	if err := h.auth.RenamePasskey(c.Request.Context(), currentUserID(c), id, name); err != nil {
		h.renderPasskeys(c, http.StatusBadRequest, i18n.T(c.Request.Context(), "settings.err_passkey_generic"))
		return
	}
	d := h.passkeyData(c)
	d.PasskeyOK = i18n.T(c.Request.Context(), "settings.ok_passkey_renamed")
	render(c, http.StatusOK, views.AccountPasskeysCard(d))
}

// DeletePasskey removes a credential.
func (h *Handlers) DeletePasskey(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		h.renderPasskeys(c, http.StatusBadRequest, i18n.T(ctx, "settings.err_passkey_generic"))
		return
	}
	switch err := h.auth.DeletePasskey(ctx, currentUserID(c), id); {
	case err == nil:
	case errors.Is(err, auth.ErrLastFactor):
		// Removing it would leave nothing that can sign in, so it is refused
		// with the reason rather than silently stranding the account.
		h.renderPasskeys(c, http.StatusBadRequest, i18n.T(ctx, "settings.err_passkey_last"))
		return
	default:
		h.renderPasskeys(c, http.StatusBadRequest, i18n.T(ctx, "settings.err_passkey_generic"))
		return
	}
	d := h.passkeyData(c)
	d.PasskeyOK = i18n.T(ctx, "settings.ok_passkey_removed")
	render(c, http.StatusOK, views.AccountPasskeysCard(d))
}

func (h *Handlers) renderPasskeys(c *gin.Context, status int, errMsg string) {
	d := h.passkeyData(c)
	d.PasskeyErr = errMsg
	render(c, status, views.AccountPasskeysCard(d))
}

// passkeyData builds the passkey card's view model.
func (h *Handlers) passkeyData(c *gin.Context) views.PasskeysData {
	uid := currentUserID(c)
	creds, err := h.auth.ListPasskeys(c.Request.Context(), uid)
	if err != nil {
		_ = c.Error(err)
	}
	out := views.PasskeysData{Available: h.auth.PasskeysAvailable()}
	for _, cr := range creds {
		out.Passkeys = append(out.Passkeys, views.PasskeyView{
			ID:           cr.ID,
			Name:         cr.Name,
			Created:      cr.CreatedAt,
			LastUsed:     cr.LastUsedAt,
			CloneWarning: cr.CloneWarning,
			// A credential the authenticator cannot back up lives and dies with
			// one device, which is worth telling the user before they rely on it.
			SyncedBackup: cr.BackupState,
		})
	}
	return out
}

func (h *Handlers) setCeremonyCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(ceremonyCookie, token, 300, "/", "", h.secure, true)
}

func (h *Handlers) clearCeremonyCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(ceremonyCookie, "", -1, "/", "", h.secure, true)
}

// writePasskeyError answers the JavaScript driving the ceremony. The message is
// deliberately coarse: the browser shows its own prompt for most failures, and
// naming which part of the assertion failed only helps someone probing.
func writePasskeyError(c *gin.Context, status int, code string, err error) {
	if status >= http.StatusInternalServerError {
		_ = c.Error(err)
	}
	c.JSON(status, gin.H{"error": gin.H{
		"code":    code,
		"message": i18n.T(c.Request.Context(), "auth.err_passkey_failed"),
	}})
}
