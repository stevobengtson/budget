package handlers

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

// render writes a Templ component to the Gin response writer with the
// supplied status code.
func render(c *gin.Context, status int, comp templ.Component) {
	c.Status(status)
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(c.Request.Context(), c.Writer); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
	}
}
