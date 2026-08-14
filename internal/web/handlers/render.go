package handlers

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/store"
)

// Render is render for the router package, whose middleware also has to emit
// HTML (a throttled request, a step-up prompt).
func Render(c *gin.Context, status int, comp templ.Component) { render(c, status, comp) }

// render writes a Templ component to the Gin response writer with the
// supplied status code.
func render(c *gin.Context, status int, comp templ.Component) {
	c.Status(status)
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(c.Request.Context(), c.Writer); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
	}
}

// writeStoreErr maps a store error to an HTTP status. A cross-tenant foreign-key
// reference (store.ErrNotOwned) is a client error; anything else is a 500.
func writeStoreErr(c *gin.Context, err error) {
	if errors.Is(err, store.ErrNotOwned) {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.String(http.StatusInternalServerError, err.Error())
}
