package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/avatar"
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
		if errors.Is(err, auth.ErrEmailNotVerified) {
			msg = "Please verify your email before logging in."
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
	render(c, http.StatusOK, views.AccountPage(h.accountData(c, "account")))
}

// accountData builds the settings view model for the given active tab, loading
// the current user so name/email are fresh (e.g. right after a profile save).
func (h *Handlers) accountData(c *gin.Context, activeTab string) views.AccountData {
	u, _ := h.store.GetUserByID(c.Request.Context(), currentUserID(c))
	return views.AccountData{
		Name:          u.Name,
		Email:         u.Email,
		AvatarVersion: u.AvatarVersion(),
		ActiveTab:     activeTab,
		Collapsed:     sidebarCollapsed(c),
	}
}

// UpdateAvatar accepts a multipart image upload, normalizes it (square 256px
// PNG), stores it, and re-renders the Account tab.
func (h *Handlers) UpdateAvatar(c *gin.Context) {
	uid := currentUserID(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarBytes+1024)

	fail := func(status int, msg string) {
		d := h.accountData(c, "account")
		d.AvatarErr = msg
		render(c, status, views.AccountPage(d))
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
	render(c, http.StatusOK, views.AccountPage(d))
}

// RemoveAvatar clears the user's custom avatar, reverting to the monogram.
func (h *Handlers) RemoveAvatar(c *gin.Context) {
	uid := currentUserID(c)
	if err := h.store.RemoveUserAvatar(c.Request.Context(), uid); err != nil {
		d := h.accountData(c, "account")
		d.AvatarErr = "Could not remove your avatar."
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}
	// Drop the avatar from this render's sidebar too (back to the monogram).
	c.Request = c.Request.WithContext(views.WithUserAvatarVersion(c.Request.Context(), 0))
	d := h.accountData(c, "account")
	d.AvatarOK = "Avatar removed."
	render(c, http.StatusOK, views.AccountPage(d))
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
		render(c, http.StatusBadRequest, views.AccountPage(d))
		return
	}
	if err := h.store.UpdateUserName(c.Request.Context(), uid, name); err != nil {
		d := h.accountData(c, "account")
		d.ProfileErr = "Could not save your profile."
		render(c, http.StatusInternalServerError, views.AccountPage(d))
		return
	}
	// Reflect the new name in this render's sidebar (requireAuth set the context
	// from the pre-update user).
	c.Request = c.Request.WithContext(views.WithUserName(c.Request.Context(), name))
	d := h.accountData(c, "account")
	d.ProfileOK = "Profile updated."
	render(c, http.StatusOK, views.AccountPage(d))
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	uid := currentUserID(c)
	cur := c.PostForm("current")
	next := c.PostForm("next")
	d := h.accountData(c, "security")
	if len(next) < 8 {
		d.PasswordErr = "New password must be at least 8 characters."
		render(c, http.StatusBadRequest, views.AccountPage(d))
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), uid, cur, next); err != nil {
		d.PasswordErr = "Current password is incorrect."
		render(c, http.StatusBadRequest, views.AccountPage(d))
		return
	}
	d.PasswordOK = "Password changed."
	render(c, http.StatusOK, views.AccountPage(d))
}
