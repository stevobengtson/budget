package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
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

// CategoriesEdit returns the modal form populated with the category's
// current name, group, goal, and due date. System (Income) categories are
// not editable.
func (h *Handlers) CategoriesEdit(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	cats, err := h.store.ListCategories(ctx, false)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var found *store.Category
	for i := range cats {
		if cats[i].ID == id {
			found = &cats[i]
			break
		}
	}
	if found == nil {
		c.String(http.StatusNotFound, "category not found")
		return
	}
	if found.IsIncome {
		c.String(http.StatusForbidden, "system categories cannot be edited")
		return
	}
	groups, _ := h.store.ListGroups(ctx)

	form := views.CategoryFormData{
		ID:      found.ID,
		GroupID: found.GroupID,
		Name:    found.Name,
		Groups:  groups,
	}
	if found.GoalCents != nil {
		form.GoalCents = money.Format(*found.GoalCents)
	}
	if found.GoalDueDate != nil {
		form.GoalDue = found.GoalDueDate.Format("2006-01-02")
	}
	render(c, http.StatusOK, views.CategoryForm(form))
}

// CategoriesUpdate writes form changes back and reloads the page so the
// row, its group placement, and any goal indicators all re-render.
func (h *Handlers) CategoriesUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	cats, err := h.store.ListCategories(ctx, false)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var cur *store.Category
	for i := range cats {
		if cats[i].ID == id {
			cur = &cats[i]
			break
		}
	}
	if cur == nil {
		c.String(http.StatusNotFound, "category not found")
		return
	}
	if cur.IsIncome {
		c.String(http.StatusForbidden, "system categories cannot be edited")
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	gid, _ := strconv.ParseInt(c.PostForm("group_id"), 10, 64)
	if gid == 0 {
		gid = cur.GroupID
	}

	updated := store.Category{
		ID:        cur.ID,
		GroupID:   gid,
		Name:      name,
		SortOrder: cur.SortOrder,
	}
	if v := strings.TrimSpace(c.PostForm("goal_cents")); v != "" {
		if cents, err := money.Parse(v); err == nil {
			updated.GoalCents = &cents
		}
	}
	if v := strings.TrimSpace(c.PostForm("goal_due")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			updated.GoalDueDate = &t
		}
	}
	if err := h.store.UpdateCategory(ctx, updated); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/categories")
	c.Writer.WriteHeader(http.StatusOK)
}
