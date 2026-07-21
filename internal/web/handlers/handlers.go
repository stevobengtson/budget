// Package handlers contains all Gin handlers used by the web server.
//
// Each handler reads/writes via the store and returns either a full
// Templ-rendered page or a partial fragment for HTMX swap.
package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/store"
)

// Handlers is a struct of all HTTP handlers; constructed once per process.
type Handlers struct {
	store         *store.Store
	auth          *auth.Service
	secure        bool // Secure flag on the session cookie
	sessionMaxAge int  // session cookie Max-Age in seconds (mirrors auth session TTL)
}

// New constructs a Handlers wired to the supplied store and auth service.
// sessionMaxAge is the session cookie lifetime in seconds.
func New(s *store.Store, a *auth.Service, secure bool, sessionMaxAge int) *Handlers {
	return &Handlers{store: s, auth: a, secure: secure, sessionMaxAge: sessionMaxAge}
}

// sidebarCollapsed reports whether the sidebar should render collapsed, from the
// templUI-written sidebar_state cookie ("true" = expanded, "false" = collapsed).
// Absent cookie defaults to expanded.
func sidebarCollapsed(c *gin.Context) bool {
	v, err := c.Cookie("sidebar_state")
	return err == nil && v == "false"
}

// incomeCollapsed reports whether the budget's Income section should render
// collapsed, from the budget_income_collapsed cookie ("true" = collapsed) that
// income-collapse.js writes on manual toggle. Absent cookie defaults to
// expanded. Transient expansions (Add Income, copy-from-prev) deliberately do
// not touch this cookie, so they don't change the remembered state.
func incomeCollapsed(c *gin.Context) bool {
	v, err := c.Cookie("budget_income_collapsed")
	return err == nil && v == "true"
}

// creditCollapsed reports whether the budget's Credit section should render
// collapsed, from the budget_credit_collapsed cookie ("true" = collapsed).
// Absent cookie defaults to expanded.
func creditCollapsed(c *gin.Context) bool {
	v, err := c.Cookie("budget_credit_collapsed")
	return err == nil && v == "true"
}
