package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/web/views"
)

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
		Name:      u.Name,
		Email:     u.Email,
		ActiveTab: activeTab,
		Collapsed: sidebarCollapsed(c),
	}
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
