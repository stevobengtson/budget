// Package api hosts the JSON HTTP API consumed by the web (and future mobile)
// clients. Authentication is delegated entirely to BetterAuth: every /api/v1
// route is guarded by JWKS verification middleware, and handlers scope data to
// the authenticated user id taken from the token's `sub` claim.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/api/handlers"
	"github.com/sbengtson/budget/internal/api/middleware"
	"github.com/sbengtson/budget/internal/core/store"
)

// Server wires the store and auth verifier into a Gin router.
type Server struct {
	store  *store.Store
	engine *gin.Engine
}

// NewServer constructs the API router. The Verifier guards all /api/v1 routes.
func NewServer(s *store.Store, v *middleware.Verifier) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	srv := &Server{store: s, engine: r}
	srv.routes(v)
	return srv
}

// Handler exposes the router as an http.Handler.
func (s *Server) Handler() http.Handler { return s.engine }

func (s *Server) routes(v *middleware.Verifier) {
	// Unauthenticated liveness probe.
	s.engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authed := s.engine.Group("/api/v1", v.Middleware())

	// Reference endpoint: echoes the authenticated user id from the JWT. Proves
	// the auth bridge independently of any store access.
	authed.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"userId": middleware.UserID(c)})
	})

	h := handlers.New(s.store)
	authed.GET("/accounts", h.ListAccounts)
	authed.POST("/accounts", h.CreateAccount)
}
