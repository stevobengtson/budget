package apiv1

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
)

// maxCeremonyBytes caps an authenticator response before parsing. A few
// kilobytes in practice; the limit stops an unauthenticated endpoint being
// handed an arbitrarily large body.
const maxCeremonyBytes = 64 << 10

// ceremonyRequest carries the token minted by a begin call plus the raw
// authenticator response.
//
// The native clients hold the ceremony token themselves rather than relying on
// a cookie: there is no cookie jar in either app, and the token is exactly the
// kind of short-lived opaque value a bearer client is already built to carry.
type ceremonyRequest struct {
	Ceremony string          `json:"ceremony"`
	Response json.RawMessage `json:"response"`
	Client   string          `json:"client"`
	Name     string          `json:"name"`
}

// PasskeyLoginBegin starts a discoverable sign-in and returns the options the
// platform API expects, verbatim.
func (a *API) PasskeyLoginBegin(c *gin.Context) {
	token, options, err := a.auth.BeginPasskeyLogin(c.Request.Context(), store.SessionInfo{
		UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "mobile",
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "passkeys_unavailable",
			"Passkeys are not available on this server.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ceremony": token, "options": json.RawMessage(options)})
}

// PasskeyLoginFinish validates the assertion and issues a session.
func (a *API) PasskeyLoginFinish(c *gin.Context) {
	req, ok := readCeremony(c)
	if !ok {
		return
	}
	client := req.Client
	if client != "ios" && client != "android" {
		client = "mobile"
	}
	r, err := a.auth.FinishPasskeyLogin(c.Request.Context(), req.Ceremony, req.Response,
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: client})
	if err != nil {
		writeError(c, http.StatusUnauthorized, "passkey_rejected",
			i18n.T(c.Request.Context(), "auth.err_passkey_failed"))
		return
	}
	// An assertion the authenticator did not verify proves possession only, so
	// the account's second factor still applies — the same rule the web flow
	// follows, because both go through one service method.
	if r.NeedsChallenge() {
		c.JSON(http.StatusOK, challengeResponse{
			Status:      "mfa_required",
			Challenge:   r.ChallengeToken,
			Methods:     factorStrings(r.Methods),
			MaskedEmail: r.MaskedEmail,
		})
		return
	}
	a.writeSession(c, r.SessionToken)
}

// PasskeyRegisterBegin starts enrolling a passkey for the signed-in user.
func (a *API) PasskeyRegisterBegin(c *gin.Context) {
	token, options, err := a.auth.BeginPasskeyRegistration(c.Request.Context(), c.GetInt64(contextUserID),
		store.SessionInfo{UserAgent: c.Request.UserAgent(), IP: c.ClientIP(), Client: "mobile"})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "passkeys_unavailable",
			"Passkeys are not available on this server.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ceremony": token, "options": json.RawMessage(options)})
}

// PasskeyRegisterFinish stores the new credential.
func (a *API) PasskeyRegisterFinish(c *gin.Context) {
	req, ok := readCeremony(c)
	if !ok {
		return
	}
	if err := a.auth.FinishPasskeyRegistration(c.Request.Context(), c.GetInt64(contextUserID),
		req.Ceremony, req.Response, req.Name); err != nil {
		writeError(c, http.StatusBadRequest, "passkey_rejected",
			i18n.T(c.Request.Context(), "auth.err_passkey_failed"))
		return
	}
	c.Status(http.StatusNoContent)
}

// Passkeys lists the account's registered credentials.
func (a *API) Passkeys(c *gin.Context) {
	creds, err := a.auth.ListPasskeys(c.Request.Context(), c.GetInt64(contextUserID))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load your passkeys.")
		return
	}
	out := make([]gin.H, 0, len(creds))
	for _, cr := range creds {
		out = append(out, gin.H{
			"id":           cr.ID,
			"name":         cr.Name,
			"createdAt":    cr.CreatedAt,
			"lastUsedAt":   cr.LastUsedAt,
			"backedUp":     cr.BackupState,
			"cloneWarning": cr.CloneWarning,
		})
	}
	c.JSON(http.StatusOK, gin.H{"passkeys": out})
}

// readCeremony parses and bounds a ceremony request body.
func readCeremony(c *gin.Context) (ceremonyRequest, bool) {
	var req ceremonyRequest
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCeremonyBytes))
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Could not read the request body.")
		return req, false
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Ceremony == "" || len(req.Response) == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request",
			"Expected a JSON body with ceremony and response.")
		return req, false
	}
	return req, true
}
