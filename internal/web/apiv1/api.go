// Package apiv1 serves the JSON API consumed by the native iOS + Android apps.
// It is a thin JSON adapter over the same store/auth/billing services the HTML
// web UI uses — no business logic lives here that isn't already in
// internal/core.
package apiv1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/store"
)

// API holds the services shared by the JSON handlers.
type API struct {
	store   *store.Store
	auth    *auth.Service
	billing *billing.Service
}

// New constructs the API wired to the same store/auth/billing the web server uses.
func New(s *store.Store, a *auth.Service, b *billing.Service) *API {
	return &API{store: s, auth: a, billing: b}
}

// Register mounts the v1 API under the given router group (expected: /api/v1).
func (a *API) Register(r *gin.RouterGroup) {
	r.GET("/health", a.Health)
	r.POST("/login", a.Login)

	authed := r.Group("")
	authed.Use(a.RequireBearerAuth())
	authed.POST("/logout", a.Logout)
	authed.GET("/me", a.Me)
	authed.GET("/budget", a.Budget)
	authed.POST("/budget/assign", a.Assign)
}

// Health is an unauthenticated liveness check the apps hit first, to prove the
// base URL + network path are good before attempting login.
func (a *API) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
