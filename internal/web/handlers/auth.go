package handlers

import (
	"errors"
	"net/http"

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
	email, _ := c.Get("userEmail")
	render(c, http.StatusOK, views.AccountPage(toString(email), "", "", sidebarCollapsed(c)))
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	emailVal, _ := c.Get("userEmail")
	email := toString(emailVal)
	uid := currentUserID(c)
	cur := c.PostForm("current")
	next := c.PostForm("next")
	if len(next) < 8 {
		render(c, http.StatusBadRequest, views.AccountPage(email, "New password must be at least 8 characters.", "", sidebarCollapsed(c)))
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), uid, cur, next); err != nil {
		render(c, http.StatusBadRequest, views.AccountPage(email, "Current password is incorrect.", "", sidebarCollapsed(c)))
		return
	}
	render(c, http.StatusOK, views.AccountPage(email, "", "Password changed.", sidebarCollapsed(c)))
}

func toString(v any) string { s, _ := v.(string); return s }
