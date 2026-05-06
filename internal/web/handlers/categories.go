package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/web/views"
)

func (h *Handlers) CategoriesIndex(c *gin.Context) {
	ctx := c.Request.Context()
	groups, _ := h.store.ListGroups(ctx)
	cats, _ := h.store.ListCategories(ctx, false)
	render(c, http.StatusOK, views.CategoriesPage(views.CategoriesData{Groups: groups, Cats: cats}))
}

func (h *Handlers) CategoriesCreateGroup(c *gin.Context) {
	name := c.PostForm("name")
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	if _, err := h.store.CreateGroup(c.Request.Context(), name, 0); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/categories")
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) CategoriesCreate(c *gin.Context) {
	gid, _ := strconv.ParseInt(c.PostForm("group_id"), 10, 64)
	name := c.PostForm("name")
	if name == "" || gid == 0 {
		c.String(http.StatusBadRequest, "name and group required")
		return
	}
	cat := store.Category{GroupID: gid, Name: name}
	if v := c.PostForm("goal_cents"); v != "" {
		if cents, err := money.Parse(v); err == nil {
			cat.GoalCents = &cents
		}
	}
	if v := c.PostForm("goal_due"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			cat.GoalDueDate = &t
		}
	}
	if _, err := h.store.CreateCategory(c.Request.Context(), cat); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/categories")
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) CategoriesArchive(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.ArchiveCategory(c.Request.Context(), id); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Writer.WriteHeader(http.StatusOK)
}
