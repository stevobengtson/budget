// Package handlers contains all Gin handlers used by the web server.
//
// Each handler reads/writes via the store and returns either a full
// Templ-rendered page or a partial fragment for HTMX swap.
package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/store"
)

// Handlers is a struct of all HTTP handlers; constructed once per process.
type Handlers struct {
	store         *store.Store
	auth          *auth.Service
	billing       *billing.Service
	secure        bool   // Secure flag on the session cookie
	sessionMaxAge int    // session cookie Max-Age in seconds (mirrors auth session TTL)
	baseURL       string // public origin, for the absolute URLs a sitemap requires
}

// New constructs a Handlers wired to the supplied store, auth, and billing
// services. sessionMaxAge is the session cookie lifetime in seconds; baseURL is
// the site's public origin, which the sitemap needs because sitemaps.org
// requires absolute URLs.
func New(s *store.Store, a *auth.Service, b *billing.Service, secure bool, sessionMaxAge int, baseURL string) *Handlers {
	return &Handlers{
		store: s, auth: a, billing: b,
		secure: secure, sessionMaxAge: sessionMaxAge, baseURL: baseURL,
	}
}

// sidebarCollapsed reports whether the sidebar should render collapsed, from the
// templUI-written sidebar_state cookie ("true" = expanded, "false" = collapsed).
// Absent cookie defaults to expanded.
func sidebarCollapsed(c *gin.Context) bool {
	v, err := c.Cookie("sidebar_state")
	return err == nil && v == "false"
}
