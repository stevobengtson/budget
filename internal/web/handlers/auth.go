package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/avatar"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// maxAvatarBytes caps an uploaded avatar before it's decoded, to bound memory.
const maxAvatarBytes = 10 << 20 // 10 MiB

func (h *Handlers) LoginForm(c *gin.Context) { render(c, http.StatusOK, views.LoginPage("", "")) }

func (h *Handlers) Login(c *gin.Context) {
	email := c.PostForm("email")
	pw := c.PostForm("password")
	r, err := h.auth.BeginLogin(c.Request.Context(), email, pw, store.SessionInfo{
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
		Client:    "web",
	})
	if err != nil {
		// A locked account is told so, with the wait. This leaks only that the
		// address is being hammered — which the attacker already knows — and
		// never whether it is registered, because unknown addresses lock on the
		// same schedule.
		if wait, locked := auth.LockedOut(err); locked {
			c.Header("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			render(c, http.StatusTooManyRequests,
				views.LoginPageLocked(email, int(wait.Minutes())+1))
			return
		}
		errKey := "auth.err_invalid_credentials"
		switch {
		case errors.Is(err, auth.ErrEmailNotVerified):
			errKey = "auth.err_verify_first"
		case errors.Is(err, auth.ErrAccountDisabled):
			errKey = "auth.err_disabled"
		}
		render(c, http.StatusUnauthorized, views.LoginPage(email, errKey))
		return
	}
	// The password was right but a second factor is outstanding. No cookie is
	// set: the challenge token lives in the form, so a half-finished sign-in
	// cannot be mistaken for a session by anything downstream.
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

func (h *Handlers) SignupForm(c *gin.Context) { render(c, http.StatusOK, views.SignupPage("", "")) }

func (h *Handlers) Signup(c *gin.Context) {
	email := c.PostForm("email")
	pw := c.PostForm("password")
	if err := h.auth.Register(c.Request.Context(), email, pw); err != nil {
		if errors.Is(err, auth.ErrPasswordTooShort) || errors.Is(err, auth.ErrPasswordTooLong) {
			render(c, http.StatusBadRequest, views.SignupPage(email, "auth.err_password_short"))
			return
		}
		if errors.Is(err, auth.ErrEmailTaken) {
			render(c, http.StatusConflict, views.SignupPage(email, "auth.err_email_taken"))
			return
		}
		_ = c.Error(err)
		render(c, http.StatusInternalServerError, views.SignupPage(email, "common.err_generic_retry"))
		return
	}
	render(c, http.StatusOK, views.MessagePage("auth.msg_verify_title", "auth.msg_verify_body",
		i18n.M{"Email": email}, "auth.msg_verify_prompt"))
}

func (h *Handlers) Verify(c *gin.Context) {
	token := c.Query("token")
	if err := h.auth.VerifyEmail(c.Request.Context(), token); err != nil {
		render(c, http.StatusBadRequest, views.MessagePage("auth.msg_verify_failed_title", "auth.msg_verify_failed_body", nil, ""))
		return
	}
	render(c, http.StatusOK, views.MessagePage("auth.msg_verified_title", "auth.msg_verified_body", nil, ""))
}

func (h *Handlers) ForgotForm(c *gin.Context) { render(c, http.StatusOK, views.ForgotPage("")) }

func (h *Handlers) Forgot(c *gin.Context) {
	_ = h.auth.RequestPasswordReset(c.Request.Context(), c.PostForm("email"))
	render(c, http.StatusOK, views.MessagePage("auth.msg_check_email_title", "auth.msg_check_email_body",
		nil, "auth.remember_password"))
}

func (h *Handlers) ResetForm(c *gin.Context) {
	render(c, http.StatusOK, views.ResetPage(c.Query("token"), ""))
}

func (h *Handlers) Reset(c *gin.Context) {
	token := c.PostForm("token")
	pw := c.PostForm("password")

	if err := h.auth.ResetPassword(c.Request.Context(), token, pw); err != nil {
		// A rejected password must not be reported as a bad link: the link is
		// fine, and telling the user otherwise sends them to request another
		// one that will fail in exactly the same way.
		errKey := "auth.err_reset_invalid"
		if errors.Is(err, auth.ErrPasswordTooShort) || errors.Is(err, auth.ErrPasswordTooLong) {
			errKey = "auth.err_password_short"
		}
		render(c, http.StatusBadRequest, views.ResetPage(token, errKey))
		return
	}
	render(c, http.StatusOK, views.MessagePage("auth.msg_reset_title", "auth.msg_reset_body", nil, ""))
}

func (h *Handlers) Logout(c *gin.Context) {
	if raw, err := c.Cookie(SessionCookieName); err == nil {
		_ = h.auth.Logout(c.Request.Context(), raw)
	}
	ClearSessionCookie(c, h.secure)
	// htmx (the sidebar user menu posts via hx-post) needs an HX-Redirect header
	// to navigate; a plain form POST (the account page) follows the 303.
	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Redirect", "/login")
		c.Status(http.StatusNoContent)
		return
	}
	c.Redirect(http.StatusSeeOther, "/login")
}

func (h *Handlers) AccountPage(c *gin.Context) {
	d := h.accountData(c, settingsTab(c))
	// Built here and nowhere else: the settings mutation handlers re-render this
	// page too, and none of them changes the subscription. When it is absent the
	// Billing tab fetches itself (AccountBillingSection).
	b := h.billingData(c)
	d.Billing = &b
	render(c, http.StatusOK, views.AccountPage(d))
}

// AccountBillingSection serves the Billing tab on its own, for the renders that
// do not build the subscription state up front.
func (h *Handlers) AccountBillingSection(c *gin.Context) {
	render(c, http.StatusOK, views.BillingSection(h.billingData(c)))
}

// settingsTab picks the initially-open settings tab from ?tab=, so the user
// menu can link straight to Billing. The tabs themselves are client-side; this
// only chooses which one is open on arrival. An unknown value falls back to the
// first tab rather than rendering none.
func settingsTab(c *gin.Context) string {
	switch c.Query("tab") {
	case "security":
		return "security"
	case "addons":
		return "addons"
	case "billing":
		return "billing"
	default:
		return "account"
	}
}

// accountData builds the settings view model for the given active tab, loading
// the current user so name/email are fresh (e.g. right after a profile save).
func (h *Handlers) accountData(c *gin.Context, activeTab string) views.AccountData {
	uid := currentUserID(c)
	u, _ := h.store.GetUserByID(c.Request.Context(), uid)
	pending, _ := h.store.GetPendingEmail(c.Request.Context(), uid)
	addOns, _ := h.store.ListAddOnsForUser(c.Request.Context(), uid)
	// bank_sync needs a configured Plaid environment to do anything; hide the
	// row entirely where there is none, rather than offering a dead toggle.
	if !h.plaid.Enabled() {
		filtered := addOns[:0]
		for _, a := range addOns {
			if a.Slug != "bank_sync" {
				filtered = append(filtered, a)
			}
		}
		addOns = filtered
	}
	d := views.AccountData{
		Name:          u.Name,
		Email:         u.Email,
		PendingEmail:  pending,
		AvatarVersion: u.AvatarVersion(),
		AddOns:        addOns,
		ActiveTab:     activeTab,
		Collapsed:     sidebarCollapsed(c),
	}
	// Only the Security tab renders the sessions card, and listing sessions is a
	// query per render — so it is loaded for that tab and skipped for the rest.
	// Loaded regardless of which tab is selected. The tabs are client-side, so
	// one page load renders every panel into the DOM and clicking a tab only
	// reveals what is already there — gating this on activeTab meant arriving
	// at plain /account and clicking Security showed a card built from an empty
	// view model, which reads as "two-step verification is unavailable".
	d.SessionsData = h.sessionsData(c)
	// Enrolment is genuinely unavailable when the server has no encryption key,
	// in which case the card says so rather than offering a dead button.
	d.Security.Available = h.auth.TwoFactorAvailable()
	if st, err := h.auth.SecurityOverview(c.Request.Context(), uid); err == nil {
		d.Security.TOTPEnabled = st.TOTPEnabled
		d.Security.EmailOTPEnabled = st.EmailOTPEnabled
		d.Security.RecoveryRemaining = st.RecoveryRemaining
	}
	return d
}

// ToggleAddOn enables or disables an add-on for the current user from the
// Add-ons tab. The switch submits its enclosing form on change; a checked
// switch posts enabled=on, an unchecked one posts nothing.
func (h *Handlers) ToggleAddOn(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	slug := c.Param("slug")
	enabled := c.PostForm("enabled") == "on"

	// bank_sync is the first PAID add-on: enabling attaches its price as a
	// second item on the base subscription, disabling detaches it (prorated
	// credit). Comped users skip Stripe entirely — the comp covers add-ons.
	// Generalize to a slug→price map when a second paid add-on exists.
	if slug == "bank_sync" && h.billing.BankSyncPriced() {
		exempt, err := h.store.IsBillingExempt(ctx, uid)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if !exempt {
			if enabled {
				err = h.billing.AttachAddOnItem(ctx, uid, h.billing.BankSyncPriceID())
			} else {
				err = h.billing.DetachAddOnItem(ctx, uid, h.billing.BankSyncPriceID())
			}
			if err != nil {
				d := h.accountData(c, "addons")
				if errors.Is(err, billing.ErrNoBaseSubscription) {
					d.AddOnErr = i18n.T(ctx, "settings.err_addon_needs_subscription")
				} else {
					d.AddOnErr = i18n.T(ctx, "settings.err_addon_billing")
				}
				render(c, http.StatusBadGateway, views.AccountAddOnsSection(d))
				return
			}
		}
	}

	if err := h.store.SetAddOnEnabled(c.Request.Context(), uid, slug, enabled); err != nil {
		d := h.accountData(c, "addons")
		if errors.Is(err, store.ErrAddOnNotFound) {
			d.AddOnErr = i18n.T(ctx, "settings.err_addon_missing")
			render(c, http.StatusNotFound, views.AccountAddOnsSection(d))
			return
		}
		d.AddOnErr = i18n.T(ctx, "settings.err_addon_update")
		render(c, http.StatusInternalServerError, views.AccountAddOnsSection(d))
		return
	}

	// Refresh the enabled set on this render's context so the sidebar nav reflects
	// the toggle immediately (requireAuth set it from the pre-toggle state).
	slugs, _ := h.store.EnabledAddOnSlugs(c.Request.Context(), uid)
	c.Request = c.Request.WithContext(views.WithEnabledAddOns(c.Request.Context(), slugs))
	// The sidebar's accounts panel carries add-on-dependent controls (bank_sync's
	// "Link a bank" button and account menu items); poke it to refetch, the same
	// event every account mutation uses.
	c.Header("HX-Trigger", "accountsChanged")

	d := h.accountData(c, "addons")
	if enabled {
		d.AddOnOK = i18n.T(ctx, "settings.ok_addon_enabled")
	} else {
		d.AddOnOK = i18n.T(ctx, "settings.ok_addon_disabled")
	}
	render(c, http.StatusOK, views.AddOnToggleResult(d))
}

// RequestEmailChange starts a verified email change from the Account tab: it
// re-checks the password and emails a confirmation link to the new address. The
// login email is unchanged until that link is confirmed.
func (h *Handlers) RequestEmailChange(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	newEmail := c.PostForm("new_email")
	password := c.PostForm("password")

	err := h.auth.RequestEmailChange(c.Request.Context(), uid, newEmail, password)
	d := h.accountData(c, "account")
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			d.EmailErr = i18n.T(ctx, "auth.err_wrong_password")
		case errors.Is(err, auth.ErrInvalidEmail):
			d.EmailErr = i18n.T(ctx, "settings.err_email_invalid")
		case errors.Is(err, auth.ErrSameEmail):
			d.EmailErr = i18n.T(ctx, "settings.err_email_same")
		case errors.Is(err, auth.ErrEmailTaken):
			d.EmailErr = i18n.T(ctx, "settings.err_email_in_use")
		default:
			d.EmailErr = i18n.T(ctx, "settings.err_email_change")
		}
		render(c, http.StatusBadRequest, views.AccountEmailSection(d))
		return
	}
	d.EmailOK = "Check " + d.PendingEmail + " for a link to confirm your new email."
	render(c, http.StatusOK, views.AccountEmailSection(d))
}

// ConfirmEmailChange applies a pending email change when the link sent to the new
// address is clicked. It is public (the click may come from a logged-out inbox);
// the token authorizes it.
func (h *Handlers) ConfirmEmailChange(c *gin.Context) {
	if err := h.auth.ConfirmEmailChange(c.Request.Context(), c.Query("token")); err != nil {
		render(c, http.StatusBadRequest, views.MessagePage("auth.msg_email_change_failed_title", "auth.msg_link_invalid", nil, ""))
		return
	}
	render(c, http.StatusOK, views.MessagePage("auth.msg_email_updated_title", "auth.msg_email_updated_body", nil, ""))
}

// UpdateAvatar accepts a multipart image upload, normalizes it (square 256px
// PNG), stores it, and re-renders the Account tab.
func (h *Handlers) UpdateAvatar(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarBytes+1024)

	fail := func(status int, msg string) {
		d := h.accountData(c, "account")
		d.AvatarErr = msg
		render(c, status, views.AccountProfileSection(d))
	}

	file, _, err := c.Request.FormFile("avatar")
	if err != nil {
		fail(http.StatusBadRequest, i18n.T(ctx, "settings.err_avatar_choose"))
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		fail(http.StatusBadRequest, i18n.T(ctx, "settings.err_avatar_read"))
		return
	}
	if len(data) > maxAvatarBytes {
		fail(http.StatusRequestEntityTooLarge, i18n.T(ctx, "settings.err_avatar_large"))
		return
	}

	png, err := avatar.Process(data)
	if err != nil {
		fail(http.StatusBadRequest, i18n.T(ctx, "settings.err_avatar_type"))
		return
	}
	if err := h.store.SetUserAvatar(c.Request.Context(), uid, png); err != nil {
		fail(http.StatusInternalServerError, i18n.T(ctx, "settings.err_avatar_save"))
		return
	}

	// Reflect the new avatar in this render's sidebar (requireAuth set the
	// context version from the pre-upload user).
	u, _ := h.store.GetUserByID(c.Request.Context(), uid)
	c.Request = c.Request.WithContext(views.WithUserAvatarVersion(c.Request.Context(), u.AvatarVersion()))
	d := h.accountData(c, "account")
	d.AvatarOK = i18n.T(ctx, "settings.ok_avatar_updated")
	render(c, http.StatusOK, views.AccountProfileSection(d))
}

// RemoveAvatar clears the user's custom avatar, reverting to the monogram.
func (h *Handlers) RemoveAvatar(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	if err := h.store.RemoveUserAvatar(c.Request.Context(), uid); err != nil {
		d := h.accountData(c, "account")
		d.AvatarErr = i18n.T(ctx, "settings.err_avatar_remove")
		render(c, http.StatusInternalServerError, views.AccountProfileSection(d))
		return
	}
	// Drop the avatar from this render's sidebar too (back to the monogram).
	c.Request = c.Request.WithContext(views.WithUserAvatarVersion(c.Request.Context(), 0))
	d := h.accountData(c, "account")
	d.AvatarOK = i18n.T(ctx, "settings.ok_avatar_removed")
	render(c, http.StatusOK, views.AccountProfileSection(d))
}

// ServeAvatar streams the current user's stored avatar PNG. The URL carries a
// ?v=<updated_at> cache-buster, so a long private cache is safe.
func (h *Handlers) ServeAvatar(c *gin.Context) {
	data, err := h.store.GetUserAvatar(c.Request.Context(), currentUserID(c))
	if err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Data(http.StatusOK, "image/png", data)
}

// UpdateProfile saves the user's display name from the Account tab.
func (h *Handlers) UpdateProfile(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		d := h.accountData(c, "account")
		d.ProfileErr = i18n.T(ctx, "settings.err_name_empty")
		render(c, http.StatusBadRequest, views.AccountProfileSection(d))
		return
	}
	if err := h.store.UpdateUserName(c.Request.Context(), uid, name); err != nil {
		d := h.accountData(c, "account")
		d.ProfileErr = i18n.T(ctx, "settings.err_profile_save")
		render(c, http.StatusInternalServerError, views.AccountProfileSection(d))
		return
	}
	// Reflect the new name in this render's sidebar (requireAuth set the context
	// from the pre-update user).
	c.Request = c.Request.WithContext(views.WithUserName(c.Request.Context(), name))
	d := h.accountData(c, "account")
	d.ProfileOK = i18n.T(ctx, "settings.ok_profile_updated")
	render(c, http.StatusOK, views.AccountProfileSection(d))
}

// StartFresh wipes all of the current user's budget data and re-provisions the
// default starter budget, keeping their account, login and subscription. It's
// gated on the current password. On success the user lands on their clean budget.
func (h *Handlers) StartFresh(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	if err := h.auth.VerifyUserPassword(c.Request.Context(), uid, c.PostForm("password")); err != nil {
		d := h.accountData(c, "account")
		d.StartFreshErr = i18n.T(ctx, "auth.err_wrong_password")
		render(c, http.StatusBadRequest, views.AccountPage(d))
		return
	}
	// Revoke bank-sync access tokens at Plaid before the wipe removes the item
	// rows — a token must not outlive the data it was consented for. Best-effort:
	// RemoveItem tolerates already-removed items, and the wipe below deletes the
	// rows regardless.
	h.plaid.RemoveAllItemsForUser(ctx, uid)
	if err := h.store.WipeUserData(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
		d := h.accountData(c, "account")
		d.StartFreshErr = i18n.T(ctx, "settings.err_start_fresh")
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}
	// Re-provision the starter budget so the user lands in a usable budget rather
	// than an empty (and Income-less) one. Seeded in the user's current language,
	// and with every group — "start fresh" restores the full starter budget, it
	// does not re-run the first-run wizard's pick-your-groups step.
	if err := h.store.SeedNewUser(c.Request.Context(), uid, i18n.LocaleFrom(ctx), nil); err != nil {
		_ = c.Error(err)
		d := h.accountData(c, "account")
		d.StartFreshErr = i18n.T(ctx, "settings.err_start_fresh_partial")
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}
	c.Redirect(http.StatusSeeOther, "/budget")
}

// DeleteAccount permanently deletes the current user and all their data. It's
// gated on the current password AND on the user re-typing their email address.
// The user's Stripe subscription is flagged to cancel at period end first
// (best-effort — a Stripe failure is logged but does not block deletion, since
// removing the account is the user's primary intent).
func (h *Handlers) DeleteAccount(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	u, err := h.store.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusInternalServerError, i18n.T(ctx, "common.err_generic"))
		return
	}

	fail := func(msg string) {
		d := h.accountData(c, "account")
		d.DeleteErr = msg
		render(c, http.StatusBadRequest, views.AccountPage(d))
	}
	if err := h.auth.VerifyUserPassword(c.Request.Context(), uid, c.PostForm("password")); err != nil {
		fail(i18n.T(ctx, "auth.err_wrong_password"))
		return
	}
	if !strings.EqualFold(strings.TrimSpace(c.PostForm("confirm_email")), u.Email) {
		fail(i18n.T(ctx, "settings.err_confirm_email_mismatch"))
		return
	}

	// Best-effort: stop the subscription renewing. Log but proceed on failure so a
	// Stripe hiccup can't trap the user in an account they've asked to delete.
	if err := h.billing.CancelAtPeriodEnd(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
	}
	// Best-effort likewise: revoke bank-sync tokens at Plaid before the delete
	// removes the item rows that hold them.
	h.plaid.RemoveAllItemsForUser(ctx, uid)

	if err := h.store.DeleteUser(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
		d := h.accountData(c, "account")
		d.DeleteErr = i18n.T(ctx, "settings.err_delete_failed")
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}

	ClearSessionCookie(c, h.secure)
	render(c, http.StatusOK, views.MessagePage("auth.msg_account_deleted_title", "auth.msg_account_deleted_body", nil, ""))
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	uid := currentUserID(c)
	cur := c.PostForm("current")
	next := c.PostForm("next")
	ctx := c.Request.Context()
	d := h.accountData(c, "security")
	// The current session is kept alive; every other device is signed out by
	// the service, which is the point of rotating a password.
	err := h.auth.ChangePassword(ctx, uid, cur, next, currentSessionToken(c))
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
		d.PasswordErr = i18n.T(ctx, "auth.err_password_short")
		render(c, http.StatusBadRequest, views.AccountPasswordCard(d))
		return
	case errors.Is(err, auth.ErrSamePassword):
		d.PasswordErr = i18n.T(ctx, "settings.err_password_same")
		render(c, http.StatusBadRequest, views.AccountPasswordCard(d))
		return
	default:
		d.PasswordErr = i18n.T(ctx, "auth.err_wrong_password")
		render(c, http.StatusBadRequest, views.AccountPasswordCard(d))
		return
	}
	d.PasswordOK = i18n.T(ctx, "settings.ok_password_changed")
	render(c, http.StatusOK, views.AccountPasswordCard(d))
}
