package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// Challenge handles the second-factor form: both verifying a code and switching
// to a different factor, since both post the same form.
func (h *Handlers) Challenge(c *gin.Context) {
	token := c.PostForm("challenge")
	method := auth.Factor(c.PostForm("method"))

	info, err := h.auth.LookupChallenge(c.Request.Context(), token)
	if err != nil {
		// An expired challenge cannot be recovered from this page, so the user
		// goes back to the start rather than staring at a form that can no
		// longer succeed.
		render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_challenge_expired"))
		return
	}
	if method == "" {
		method = info.Methods[0]
	}

	// "Switch factor" re-renders rather than verifying: the user has not
	// submitted a code yet.
	if c.PostForm("switch") == "1" {
		render(c, http.StatusOK, views.ChallengePage(views.ChallengeData{
			Token: token, Method: method, Methods: info.Methods, MaskedEmail: info.MaskedEmail,
		}))
		return
	}

	raw, err := h.auth.CompleteChallenge(c.Request.Context(), token, method, c.PostForm("code"),
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "web"})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTooManyAttempts):
			render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_too_many_attempts"))
		case errors.Is(err, auth.ErrChallengeExpired):
			render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_challenge_expired"))
		default:
			render(c, http.StatusUnauthorized, views.ChallengePage(views.ChallengeData{
				Token: token, Method: method, Methods: info.Methods,
				MaskedEmail: info.MaskedEmail, ErrKey: "auth.err_code_invalid",
			}))
		}
		return
	}
	SetSessionCookie(c, raw, h.sessionMaxAge, h.secure)
	c.Redirect(http.StatusSeeOther, "/budget")
}

// ChallengeSendCode emails a fresh code and re-renders the form.
func (h *Handlers) ChallengeSendCode(c *gin.Context) {
	token := c.PostForm("challenge")
	if err := h.auth.SendChallengeCode(c.Request.Context(), token); err != nil {
		render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_challenge_expired"))
		return
	}
	info, err := h.auth.LookupChallenge(c.Request.Context(), token)
	if err != nil {
		render(c, http.StatusUnauthorized, views.LoginPage("", "auth.err_challenge_expired"))
		return
	}
	render(c, http.StatusOK, views.ChallengePage(views.ChallengeData{
		Token: token, Method: auth.FactorEmailOTP, Methods: info.Methods, MaskedEmail: info.MaskedEmail,
	}))
}

// ChallengeCancel abandons a half-finished sign-in.
func (h *Handlers) ChallengeCancel(c *gin.Context) {
	_ = h.auth.AbandonChallenge(c.Request.Context(), c.PostForm("challenge"))
	c.Redirect(http.StatusSeeOther, "/login")
}

// BeginTOTP starts authenticator enrolment and shows the QR code.
func (h *Handlers) BeginTOTP(c *gin.Context) {
	uid := currentUserID(c)
	enr, err := h.auth.BeginTOTPEnrolment(c.Request.Context(), uid)
	if err != nil {
		h.renderTwoFactor(c, http.StatusBadRequest, twoFactorErr(c, err))
		return
	}
	d := h.securityData(c)
	d.Security.PendingURI = enr.URI
	d.Security.PendingSecret = enr.Secret
	render(c, http.StatusOK, views.AccountTwoFactorCard(d))
}

// TOTPQR serves the pending enrolment's QR image.
func (h *Handlers) TOTPQR(c *gin.Context) {
	png, err := h.auth.TOTPQRCode(c.Request.Context(), currentUserID(c))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	// The image encodes the shared secret, so it must not be cached anywhere
	// between the server and the screen.
	c.Header("Cache-Control", "no-store, private")
	c.Data(http.StatusOK, "image/png", png)
}

// ConfirmTOTP completes enrolment and reveals the recovery codes once.
func (h *Handlers) ConfirmTOTP(c *gin.Context) {
	uid := currentUserID(c)
	codes, err := h.auth.ConfirmTOTP(c.Request.Context(), uid, c.PostForm("code"))
	if err != nil {
		// Re-render the setup step, not the summary: the QR on screen is still
		// the right one, and sending the user back to the start would make them
		// re-scan for a typo.
		d := h.securityData(c)
		if enr, e := h.auth.PendingTOTPEnrolment(c.Request.Context(), uid); e == nil {
			d.Security.PendingURI = enr.URI
			d.Security.PendingSecret = enr.Secret
		}
		d.TwoFactorErr = i18n.T(c.Request.Context(), "settings.err_twofa_code")
		render(c, http.StatusBadRequest, views.AccountTwoFactorCard(d))
		return
	}
	d := h.securityData(c)
	d.TwoFactorOK = i18n.T(c.Request.Context(), "settings.ok_twofa_enabled")
	d.NewRecoveryCodes = codes
	// Both cards change: two-factor turned on, and codes now exist. The recovery
	// card is pushed out-of-band so the codes appear without a second request —
	// they exist in memory only on this response.
	render(c, http.StatusOK, views.TwoFactorWithRecovery(d))
}

// CancelTOTP discards a pending enrolment.
func (h *Handlers) CancelTOTP(c *gin.Context) {
	_, _ = h.auth.DisableTOTP(c.Request.Context(), currentUserID(c))
	h.renderTwoFactor(c, http.StatusOK, "")
}

// DisableTOTP turns the authenticator off.
func (h *Handlers) DisableTOTP(c *gin.Context) {
	change, err := h.auth.DisableTOTP(c.Request.Context(), currentUserID(c))
	if err != nil {
		h.renderTwoFactor(c, http.StatusInternalServerError, twoFactorErr(c, err))
		return
	}
	d := h.securityData(c)
	d.TwoFactorOK = disableMessage(c, change)
	// The recovery card's count changed too, so it is pushed alongside rather
	// than left showing a stale number until the next full page load.
	render(c, http.StatusOK, views.TwoFactorWithRecovery(d))
}

// disableMessage says what turning a factor off actually did. Saying only
// "two-step verification is off" would leave someone holding a printout of
// recovery codes that silently stopped working.
func disableMessage(c *gin.Context, change auth.FactorChange) string {
	ctx := c.Request.Context()
	if change.RecoveryCodesCleared {
		return i18n.T(ctx, "settings.ok_twofa_disabled_codes_cleared")
	}
	return i18n.T(ctx, "settings.ok_twofa_disabled")
}

// ToggleEmailOTP turns emailed sign-in codes on or off. The form posts the
// desired next state, so a stale page cannot flip the setting the wrong way.
func (h *Handlers) ToggleEmailOTP(c *gin.Context) {
	on := c.PostForm("enabled") == "on"
	change, err := h.auth.SetEmailOTPEnabled(c.Request.Context(), currentUserID(c), on)
	if err != nil {
		h.renderTwoFactor(c, http.StatusInternalServerError, twoFactorErr(c, err))
		return
	}
	d := h.securityData(c)
	if on {
		d.TwoFactorOK = i18n.T(c.Request.Context(), "settings.ok_twofa_enabled")
		// Turning this on can mint the account's first recovery codes. They
		// exist in memory only on this response, so if they are not shown here
		// the user has ten codes they will never see.
		if len(change.NewRecoveryCodes) > 0 {
			d.NewRecoveryCodes = change.NewRecoveryCodes
			d.RecoveryOK = i18n.T(c.Request.Context(), "settings.recovery_issued_with_factor")
			h.stashRecoveryCodes(c, change.NewRecoveryCodes)
		}
	} else {
		d.TwoFactorOK = disableMessage(c, change)
	}
	render(c, http.StatusOK, views.TwoFactorWithRecovery(d))
}

// RegenerateRecoveryCodes issues a fresh set and shows it once.
func (h *Handlers) RegenerateRecoveryCodes(c *gin.Context) {
	codes, err := h.auth.RegenerateRecoveryCodes(c.Request.Context(), currentUserID(c))
	d := h.securityData(c)
	if err != nil {
		_ = c.Error(err)
		d.RecoveryErr = i18n.T(c.Request.Context(), "settings.err_twofa_generic")
		render(c, http.StatusInternalServerError, views.AccountRecoveryCard(d))
		return
	}
	d.NewRecoveryCodes = codes
	d.RecoveryOK = i18n.T(c.Request.Context(), "settings.ok_recovery_regenerated")
	h.stashRecoveryCodes(c, codes)
	render(c, http.StatusOK, views.AccountRecoveryCard(d))
}

// DownloadRecoveryCodes serves the codes from the current response cycle as a
// text file. It reads the short-lived cookie set when they were generated,
// because the codes are never stored in a form the server can read back.
func (h *Handlers) DownloadRecoveryCodes(c *gin.Context) {
	raw, err := c.Cookie(recoveryCookie)
	if err != nil || raw == "" {
		c.Status(http.StatusNotFound)
		return
	}
	// One-shot: clear it as it is served, so the file cannot be re-fetched from
	// a browser someone else later sits down at.
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(recoveryCookie, "", -1, "/account", "", h.secure, true)

	body := strings.ReplaceAll(raw, ",", "\n") + "\n"
	c.Header("Cache-Control", "no-store, private")
	c.Header("Content-Disposition", `attachment; filename="pigglet-recovery-codes.txt"`)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

// recoveryCookie briefly carries freshly generated recovery codes so the
// download link can serve them. Scoped to /account, HttpOnly, and short-lived:
// the codes only need to survive from the response that made them to the click
// that saves them.
const recoveryCookie = "budget_recovery"

func (h *Handlers) stashRecoveryCodes(c *gin.Context, codes []string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(recoveryCookie, strings.Join(codes, ","), 600, "/account", "", h.secure, true)
}

// securityData builds the Security tab's two-factor view model.
func (h *Handlers) securityData(c *gin.Context) views.AccountData {
	d := h.accountData(c, "security")
	return d
}

// renderTwoFactor re-renders the card with an optional error.
func (h *Handlers) renderTwoFactor(c *gin.Context, status int, errMsg string) {
	d := h.securityData(c)
	d.TwoFactorErr = errMsg
	render(c, status, views.AccountTwoFactorCard(d))
}

// twoFactorErr maps a service error to a message key.
func twoFactorErr(c *gin.Context, err error) string {
	ctx := c.Request.Context()
	switch {
	case errors.Is(err, auth.ErrSealerUnavailable):
		return i18n.T(ctx, "settings.twofa_unavailable")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return i18n.T(ctx, "settings.err_twofa_code")
	default:
		_ = c.Error(err)
		return i18n.T(ctx, "settings.err_twofa_generic")
	}
}
