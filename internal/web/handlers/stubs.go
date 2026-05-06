package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Placeholder writes a stub message for handlers that haven't been
// implemented yet. Replaced as features land.
func placeholder(c *gin.Context, page string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8",
		[]byte("<!doctype html><html><body><pre>"+page+" — coming soon</pre></body></html>"))
}

// --- Budget ---
func (h *Handlers) BudgetGoal(c *gin.Context)          { placeholder(c, "BudgetGoal") }

// --- Transactions ---

// --- Accounts ---

// --- Categories ---

// --- Paydown ---

// Index handlers (placeholders until each tab is built).

func (h *Handlers) CategoriesUpdate(c *gin.Context) { placeholder(c, "CategoriesUpdate") }
