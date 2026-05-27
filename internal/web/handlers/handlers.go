// Package handlers contains all Gin handlers used by the web server.
//
// Each handler reads/writes via the store and returns either a full
// Templ-rendered page or a partial fragment for HTMX swap.
package handlers

import "github.com/sbengtson/budget/internal/core/store"

// Handlers is a struct of all HTTP handlers; constructed once per process.
type Handlers struct {
	store *store.Store
}

// New constructs a Handlers wired to the supplied store.
func New(s *store.Store) *Handlers {
	return &Handlers{store: s}
}
