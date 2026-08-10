package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/avatar"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// maxAvatarBytes caps an uploaded avatar before it's decoded, to bound memory.
const maxAvatarBytes = 10 << 20 // 10 MiB

func (h *Handlers) LoginForm(c *gin.Context) { render(c, http.StatusOK, views.LoginPage("", "")) }

func (h *Handlers) Login(c *gin.Context) {
	email := c.PostForm("email")
	pw := c.PostForm("password")
	raw, err := h.auth.Login(c.Request.Context(), email, pw, c.Request.UserAgent(), c.ClientIP())
	if err != nil {
		msg := "Invalid email or password."
		switch {
		case errors.Is(err, auth.ErrEmailNotVerified):
			msg = "Please verify your email before logging in."
		case errors.Is(err, auth.ErrAccountDisabled):
			msg = "This account has been disabled. Contact support if you think this is a mistake."
		}
		render(c, http.StatusUnauthorized, views.LoginPage(email, msg))
		return
	}
	SetSessionCookie(c, raw, h.sessionMaxAge, h.secure)
	c.Redirect(http.StatusSeeOther, "/budget")
}

func (h *Handlers) SignupForm(c *gin.Context) { render(c, http.StatusOK, views.SignupPage("", "")) }

func (h *Handlers) Signup(c *gin.Context) {
	email := c.PostForm("email")
	pw := c.PostForm("password")
	if len(pw) < 8 {
		render(c, http.StatusBadRequest, views.SignupPage(email, "Password must be at least 8 characters."))
		return
	}
	if err := h.auth.Register(c.Request.Context(), email, pw); err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			render(c, http.StatusConflict, views.SignupPage(email, "That email is already registered."))
			return
		}
		_ = c.Error(err)
		render(c, http.StatusInternalServerError, views.SignupPage(email, "Something went wrong. Try again."))
		return
	}
	render(c, http.StatusOK, views.MessagePage("Verify Email",
		"Check your email for a verification link. We sent one to "+email+".",
		"Already verified your email?"))
}

func (h *Handlers) Verify(c *gin.Context) {
	token := c.Query("token")
	if err := h.auth.VerifyEmail(c.Request.Context(), token); err != nil {
		render(c, http.StatusBadRequest, views.MessagePage("Verification Failed",
			"That link is invalid or expired. Try signing up again.", ""))
		return
	}
	render(c, http.StatusOK, views.MessagePage("Email Verified",
		"Your email is verified — you can now sign in.", ""))
}

func (h *Handlers) ForgotForm(c *gin.Context) { render(c, http.StatusOK, views.ForgotPage("")) }

func (h *Handlers) Forgot(c *gin.Context) {
	_ = h.auth.RequestPasswordReset(c.Request.Context(), c.PostForm("email"))
	render(c, http.StatusOK, views.MessagePage("Check Your Email",
		"If that email has an account, a reset link is on its way.",
		"Remember your password?"))
}

func (h *Handlers) ResetForm(c *gin.Context) {
	render(c, http.StatusOK, views.ResetPage(c.Query("token"), ""))
}

func (h *Handlers) Reset(c *gin.Context) {
	token := c.PostForm("token")
	pw := c.PostForm("password")
	if len(pw) < 8 {
		render(c, http.StatusBadRequest, views.ResetPage(token, "Password must be at least 8 characters."))
		return
	}
	if err := h.auth.ResetPassword(c.Request.Context(), token, pw); err != nil {
		render(c, http.StatusBadRequest, views.ResetPage(token, "That reset link is invalid or expired."))
		return
	}
	render(c, http.StatusOK, views.MessagePage("Password Reset",
		"Your password was updated — you can now sign in.", ""))
}

func (h *Handlers) Logout(c *gin.Context) {
	if raw, err := c.Cookie(SessionCookieName); err == nil {
		_ = h.auth.Logout(c.Request.Context(), raw)
	}
	ClearSessionCookie(c)
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
	return views.AccountData{
		Name:          u.Name,
		Email:         u.Email,
		PendingEmail:  pending,
		AvatarVersion: u.AvatarVersion(),
		AddOns:        addOns,
		ActiveTab:     activeTab,
		Collapsed:     sidebarCollapsed(c),
	}
}

// ToggleAddOn enables or disables an add-on for the current user from the
// Add-ons tab. The switch submits its enclosing form on change; a checked
// switch posts enabled=on, an unchecked one posts nothing.
func (h *Handlers) ToggleAddOn(c *gin.Context) {
	uid := currentUserID(c)
	slug := c.Param("slug")
	enabled := c.PostForm("enabled") == "on"

	if err := h.store.SetAddOnEnabled(c.Request.Context(), uid, slug, enabled); err != nil {
		d := h.accountData(c, "addons")
		if errors.Is(err, store.ErrAddOnNotFound) {
			d.AddOnErr = "That add-on doesn't exist."
			render(c, http.StatusNotFound, views.AccountAddOnsSection(d))
			return
		}
		d.AddOnErr = "Could not update the add-on."
		render(c, http.StatusInternalServerError, views.AccountAddOnsSection(d))
		return
	}

	// Refresh the enabled set on this render's context so the sidebar nav reflects
	// the toggle immediately (requireAuth set it from the pre-toggle state).
	slugs, _ := h.store.EnabledAddOnSlugs(c.Request.Context(), uid)
	c.Request = c.Request.WithContext(views.WithEnabledAddOns(c.Request.Context(), slugs))

	d := h.accountData(c, "addons")
	if enabled {
		d.AddOnOK = "Add-on enabled."
	} else {
		d.AddOnOK = "Add-on disabled."
	}
	render(c, http.StatusOK, views.AddOnToggleResult(d))
}

// RequestEmailChange starts a verified email change from the Account tab: it
// re-checks the password and emails a confirmation link to the new address. The
// login email is unchanged until that link is confirmed.
func (h *Handlers) RequestEmailChange(c *gin.Context) {
	uid := currentUserID(c)
	newEmail := c.PostForm("new_email")
	password := c.PostForm("password")

	err := h.auth.RequestEmailChange(c.Request.Context(), uid, newEmail, password)
	d := h.accountData(c, "account")
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			d.EmailErr = "Current password is incorrect."
		case errors.Is(err, auth.ErrInvalidEmail):
			d.EmailErr = "Enter a valid email address."
		case errors.Is(err, auth.ErrSameEmail):
			d.EmailErr = "That is already your email."
		case errors.Is(err, auth.ErrEmailTaken):
			d.EmailErr = "That email is already in use."
		default:
			d.EmailErr = "Could not start the email change."
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
		render(c, http.StatusBadRequest, views.MessagePage("Email Change Failed",
			"That link is invalid or expired.", ""))
		return
	}
	render(c, http.StatusOK, views.MessagePage("Email Updated",
		"Your login email has been updated.", ""))
}

// UpdateAvatar accepts a multipart image upload, normalizes it (square 256px
// PNG), stores it, and re-renders the Account tab.
func (h *Handlers) UpdateAvatar(c *gin.Context) {
	uid := currentUserID(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarBytes+1024)

	fail := func(status int, msg string) {
		d := h.accountData(c, "account")
		d.AvatarErr = msg
		render(c, status, views.AccountProfileSection(d))
	}

	file, _, err := c.Request.FormFile("avatar")
	if err != nil {
		fail(http.StatusBadRequest, "Choose an image to upload.")
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		fail(http.StatusBadRequest, "Could not read the uploaded file.")
		return
	}
	if len(data) > maxAvatarBytes {
		fail(http.StatusRequestEntityTooLarge, "Image is too large (max 10 MB).")
		return
	}

	png, err := avatar.Process(data)
	if err != nil {
		fail(http.StatusBadRequest, "That file isn't a supported image (PNG, JPEG, GIF, or WEBP).")
		return
	}
	if err := h.store.SetUserAvatar(c.Request.Context(), uid, png); err != nil {
		fail(http.StatusInternalServerError, "Could not save your avatar.")
		return
	}

	// Reflect the new avatar in this render's sidebar (requireAuth set the
	// context version from the pre-upload user).
	u, _ := h.store.GetUserByID(c.Request.Context(), uid)
	c.Request = c.Request.WithContext(views.WithUserAvatarVersion(c.Request.Context(), u.AvatarVersion()))
	d := h.accountData(c, "account")
	d.AvatarOK = "Avatar updated."
	render(c, http.StatusOK, views.AccountProfileSection(d))
}

// RemoveAvatar clears the user's custom avatar, reverting to the monogram.
func (h *Handlers) RemoveAvatar(c *gin.Context) {
	uid := currentUserID(c)
	if err := h.store.RemoveUserAvatar(c.Request.Context(), uid); err != nil {
		d := h.accountData(c, "account")
		d.AvatarErr = "Could not remove your avatar."
		render(c, http.StatusInternalServerError, views.AccountProfileSection(d))
		return
	}
	// Drop the avatar from this render's sidebar too (back to the monogram).
	c.Request = c.Request.WithContext(views.WithUserAvatarVersion(c.Request.Context(), 0))
	d := h.accountData(c, "account")
	d.AvatarOK = "Avatar removed."
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
	uid := currentUserID(c)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		d := h.accountData(c, "account")
		d.ProfileErr = "Name can't be empty."
		render(c, http.StatusBadRequest, views.AccountProfileSection(d))
		return
	}
	if err := h.store.UpdateUserName(c.Request.Context(), uid, name); err != nil {
		d := h.accountData(c, "account")
		d.ProfileErr = "Could not save your profile."
		render(c, http.StatusInternalServerError, views.AccountProfileSection(d))
		return
	}
	// Reflect the new name in this render's sidebar (requireAuth set the context
	// from the pre-update user).
	c.Request = c.Request.WithContext(views.WithUserName(c.Request.Context(), name))
	d := h.accountData(c, "account")
	d.ProfileOK = "Profile updated."
	render(c, http.StatusOK, views.AccountProfileSection(d))
}

// StartFresh wipes all of the current user's budget data and re-provisions the
// default starter budget, keeping their account, login and subscription. It's
// gated on the current password. On success the user lands on their clean budget.
func (h *Handlers) StartFresh(c *gin.Context) {
	uid := currentUserID(c)
	if err := h.auth.VerifyUserPassword(c.Request.Context(), uid, c.PostForm("password")); err != nil {
		d := h.accountData(c, "account")
		d.StartFreshErr = "Current password is incorrect."
		render(c, http.StatusBadRequest, views.AccountPage(d))
		return
	}
	if err := h.store.WipeUserData(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
		d := h.accountData(c, "account")
		d.StartFreshErr = "Could not reset your data. Please try again."
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}
	// Re-provision the starter budget so the user lands in the same clean state as
	// a brand-new account rather than an empty (and Income-less) budget.
	if err := h.store.SeedNewUser(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
		d := h.accountData(c, "account")
		d.StartFreshErr = "Your data was cleared, but re-creating the starter budget failed."
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
	uid := currentUserID(c)
	u, err := h.store.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		_ = c.Error(err)
		c.String(http.StatusInternalServerError, "Something went wrong.")
		return
	}

	fail := func(msg string) {
		d := h.accountData(c, "account")
		d.DeleteErr = msg
		render(c, http.StatusBadRequest, views.AccountPage(d))
	}
	if err := h.auth.VerifyUserPassword(c.Request.Context(), uid, c.PostForm("password")); err != nil {
		fail("Current password is incorrect.")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(c.PostForm("confirm_email")), u.Email) {
		fail("The email you typed doesn't match your account email.")
		return
	}

	// Best-effort: stop the subscription renewing. Log but proceed on failure so a
	// Stripe hiccup can't trap the user in an account they've asked to delete.
	if err := h.billing.CancelAtPeriodEnd(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
	}

	if err := h.store.DeleteUser(c.Request.Context(), uid); err != nil {
		_ = c.Error(err)
		d := h.accountData(c, "account")
		d.DeleteErr = "Could not delete your account. Please try again."
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}

	ClearSessionCookie(c)
	render(c, http.StatusOK, views.MessagePage("Account deleted",
		"Your account and all your data have been permanently deleted. We're sorry to see you go.", ""))
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	uid := currentUserID(c)
	cur := c.PostForm("current")
	next := c.PostForm("next")
	d := h.accountData(c, "security")
	if len(next) < 8 {
		d.PasswordErr = "New password must be at least 8 characters."
		render(c, http.StatusBadRequest, views.AccountSecuritySection(d))
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), uid, cur, next); err != nil {
		d.PasswordErr = "Current password is incorrect."
		render(c, http.StatusBadRequest, views.AccountSecuritySection(d))
		return
	}
	d.PasswordOK = "Password changed."
	render(c, http.StatusOK, views.AccountSecuritySection(d))
}
