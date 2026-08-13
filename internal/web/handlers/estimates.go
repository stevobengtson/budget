package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/format"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// EstimatesIndex lists the user's estimates with the new-estimate form. The
// suggested name is the current month/year in the user's locale ("Aug 2026").
func (h *Handlers) EstimatesIndex(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.store.ListEstimates(ctx, currentUserID(c))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	d := views.EstimatesListData{
		Estimates:   list,
		DefaultName: format.MonthYear(ctx, time.Now()),
	}
	render(c, http.StatusOK, views.EstimatesListPage(d, sidebarCollapsed(c)))
}

// EstimatesCreate snapshots the current budget + this month's income into a new
// estimate and opens its editor.
func (h *Handlers) EstimatesCreate(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = format.MonthYear(ctx, time.Now())
	}
	id, err := h.store.CreateEstimateSnapshot(ctx, uid, name, store.MonthKey(time.Now()))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", fmt.Sprintf("/estimates/%d", id))
	c.Status(http.StatusOK)
}

// EstimateShow renders the estimate editor.
func (h *Handlers) EstimateShow(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	d, err := h.estimateData(ctx, uid, id)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimatePage(d, sidebarCollapsed(c)))
}

// EstimateDelete removes an estimate from the list page. The row is the primary
// target and swaps to nothing.
func (h *Handlers) EstimateDelete(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteEstimate(ctx, uid, id); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(http.StatusOK)
}

// estimateData assembles the editor's view-model: the estimate, its groups with
// their categories binned in group order, and its income rows.
func (h *Handlers) estimateData(ctx context.Context, userID, estimateID int64) (views.EstimateData, error) {
	est, err := h.store.GetEstimate(ctx, userID, estimateID)
	if err != nil {
		return views.EstimateData{}, err
	}
	groups, err := h.store.ListEstimateGroups(ctx, userID, estimateID)
	if err != nil {
		return views.EstimateData{}, fmt.Errorf("estimate groups: %w", err)
	}
	cats, err := h.store.ListEstimateCategories(ctx, userID, estimateID)
	if err != nil {
		return views.EstimateData{}, fmt.Errorf("estimate categories: %w", err)
	}
	incomes, err := h.store.ListEstimateIncomes(ctx, userID, estimateID)
	if err != nil {
		return views.EstimateData{}, fmt.Errorf("estimate incomes: %w", err)
	}
	byGroup := make(map[int64][]store.EstimateCategory)
	for _, cat := range cats {
		byGroup[cat.GroupID] = append(byGroup[cat.GroupID], cat)
	}
	vg := make([]views.EstimateViewGroup, 0, len(groups))
	for _, g := range groups {
		vg = append(vg, views.EstimateViewGroup{Group: g, Rows: byGroup[g.ID]})
	}
	return views.EstimateData{Estimate: est, Groups: vg, Incomes: incomes}, nil
}

// estimateErr maps a missing/foreign estimate to a redirect back to the list
// (the estimate may have been deleted in another tab); everything else is a 500.
func (h *Handlers) estimateErr(c *gin.Context, err error) {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNotOwned) {
		if c.GetHeader("HX-Request") == "true" {
			c.Header("HX-Redirect", "/estimates")
			c.Status(http.StatusNoContent)
			return
		}
		c.Redirect(http.StatusSeeOther, "/estimates")
		return
	}
	c.String(http.StatusInternalServerError, err.Error())
}

// estimateGroupView rebuilds one group's view (header total needs its rows).
func (h *Handlers) estimateGroupView(d views.EstimateData, groupID int64) views.EstimateViewGroup {
	for _, g := range d.Groups {
		if g.Group.ID == groupID {
			return g
		}
	}
	return views.EstimateViewGroup{}
}

// --- Incomes ---

func (h *Handlers) EstimateIncomeNew(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	render(c, http.StatusOK, views.EstimateIncomeNewRow(id))
}

func (h *Handlers) EstimateIncomeCreate(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.name_required"))
		return
	}
	cents, err := money.Parse(ctx, c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.invalid_amount"))
		return
	}
	incomes, err := h.store.ListEstimateIncomes(ctx, uid, estimateID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var maxSort int64
	for _, r := range incomes {
		if r.SortOrder > maxSort {
			maxSort = r.SortOrder
		}
	}
	id, err := h.store.CreateEstimateIncome(ctx, uid, estimateID, name, cents, maxSort+1)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	row := store.EstimateIncome{ID: id, EstimateID: estimateID, Name: name, AmountCents: cents, SortOrder: maxSort + 1}
	render(c, http.StatusOK, views.EstimateIncomeRowResult(estimateID, row, d))
}

// EstimateIncomeUpdate field-merges name and/or amount (whichever the inline
// edit submitted) and returns the refreshed row + OOB summary.
func (h *Handlers) EstimateIncomeUpdate(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	iid, _ := strconv.ParseInt(c.Param("iid"), 10, 64)

	cur, err := h.store.GetEstimateIncome(ctx, uid, iid)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	if v, ok := c.GetPostForm("name"); ok {
		name := strings.TrimSpace(v)
		if name == "" {
			c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.name_required"))
			return
		}
		cur.Name = name
	}
	if v, ok := c.GetPostForm("amount"); ok {
		cents, err := money.Parse(ctx, v)
		if err != nil {
			c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.invalid_amount"))
			return
		}
		cur.AmountCents = cents
	}
	if err := h.store.UpdateEstimateIncome(ctx, uid, cur); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateIncomeRowResult(estimateID, cur, d))
}

func (h *Handlers) EstimateIncomeDelete(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	iid, _ := strconv.ParseInt(c.Param("iid"), 10, 64)
	if err := h.store.DeleteEstimateIncome(ctx, uid, iid); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateSummaryResult(d))
}

// --- Groups ---

func (h *Handlers) EstimateGroupNew(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	render(c, http.StatusOK, views.EstimateGroupNewRow(id))
}

// EstimateGroupCreate creates a group at the top (next to the Add group button,
// mirroring the budget page) and returns its group div.
func (h *Handlers) EstimateGroupCreate(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.name_required"))
		return
	}
	min, err := h.store.MinEstimateGroupSortOrder(ctx, uid, estimateID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	id, err := h.store.CreateEstimateGroup(ctx, uid, estimateID, name, min-1)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	g := views.EstimateViewGroup{Group: store.EstimateGroup{ID: id, EstimateID: estimateID, Name: name, SortOrder: min - 1}}
	render(c, http.StatusOK, views.EstimateGroupTbody(g))
}

func (h *Handlers) EstimateGroupRename(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.name_required"))
		return
	}
	if err := h.store.RenameEstimateGroup(ctx, uid, gid, name); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateGroupHeader(h.estimateGroupView(d, gid), false))
}

// EstimateGroupDelete removes the group (cascading its categories); the group
// div is the primary target and swaps to nothing, the summary refreshes OOB.
func (h *Handlers) EstimateGroupDelete(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	if err := h.store.DeleteEstimateGroup(ctx, uid, gid); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateGroupDeleteResult(d))
}

// EstimateGroupsReorder persists a new group order posted by group-sort.js as a
// comma-separated "ids" field. Nothing money-related changes, so 204.
func (h *Handlers) EstimateGroupsReorder(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var ids []int64
	for _, s := range splitNonEmpty(c.PostForm("ids"), ",") {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			c.String(http.StatusBadRequest, "bad group id %q", s)
			return
		}
		ids = append(ids, id)
	}
	if err := h.store.ReorderEstimateGroups(ctx, uid, estimateID, ids); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// --- Categories ---

func (h *Handlers) EstimateCategoryNew(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	render(c, http.StatusOK, views.EstimateCategoryNewRow(id, gid))
}

func (h *Handlers) EstimateCategoryCreate(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.name_required"))
		return
	}
	maxSort, err := h.store.MaxEstimateCategorySortOrder(ctx, uid, gid)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	id, err := h.store.CreateEstimateCategory(ctx, uid, gid, name, 0, maxSort+1)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	row := store.EstimateCategory{ID: id, GroupID: gid, Name: name, SortOrder: maxSort + 1}
	render(c, http.StatusOK, views.EstimateCategoryRow(estimateID, row))
}

func (h *Handlers) EstimateCategoryRename(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.name_required"))
		return
	}
	if err := h.store.RenameEstimateCategory(ctx, uid, catID, name); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	cur, err := h.store.GetEstimateCategory(ctx, uid, catID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateCategoryRow(estimateID, cur))
}

// EstimateAssign updates a category's assigned amount and refreshes the row,
// its group total, and the summary.
func (h *Handlers) EstimateAssign(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	cents, err := money.Parse(ctx, c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "%s", i18n.T(ctx, "err.invalid_amount"))
		return
	}
	if err := h.store.SetEstimateAssigned(ctx, uid, catID, cents); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	cur, err := h.store.GetEstimateCategory(ctx, uid, catID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateCategoryRowResult(estimateID, cur, h.estimateGroupView(d, cur.GroupID), d))
}

// EstimateCategoryDelete removes the row (empty primary target) and refreshes
// the group total + summary out of band.
func (h *Handlers) EstimateCategoryDelete(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	cur, err := h.store.GetEstimateCategory(ctx, uid, catID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	if err := h.store.DeleteEstimateCategory(ctx, uid, catID); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	render(c, http.StatusOK, views.EstimateCategoryDeleteResult(h.estimateGroupView(d, cur.GroupID), d))
}

// EstimateCategoriesReorder persists a category drag posted by category-sort.js
// ("groups"/"cats" pipe-delimited pairs, like the budget page) and refreshes the
// affected group headers — a cross-group move shifts both groups' totals.
func (h *Handlers) EstimateCategoriesReorder(c *gin.Context) {
	ctx := c.Request.Context()
	uid := currentUserID(c)
	estimateID, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	groups := strings.Split(c.PostForm("groups"), "|")
	cats := strings.Split(c.PostForm("cats"), "|")
	if len(groups) != len(cats) {
		c.String(http.StatusBadRequest, "groups/cats length mismatch")
		return
	}
	affected := make([]int64, 0, len(groups))
	for i, gs := range groups {
		gid, err := strconv.ParseInt(gs, 10, 64)
		if err != nil {
			c.String(http.StatusBadRequest, "bad group id %q", gs)
			return
		}
		var catIDs []int64
		for _, cs := range splitNonEmpty(cats[i], ",") {
			id, err := strconv.ParseInt(cs, 10, 64)
			if err != nil {
				c.String(http.StatusBadRequest, "bad category id %q", cs)
				return
			}
			catIDs = append(catIDs, id)
		}
		if err := h.store.ReorderEstimateCategories(ctx, uid, gid, catIDs); err != nil {
			writeStoreErr(c, err)
			return
		}
		affected = append(affected, gid)
	}

	d, err := h.estimateData(ctx, uid, estimateID)
	if err != nil {
		h.estimateErr(c, err)
		return
	}
	vg := make([]views.EstimateViewGroup, 0, len(affected))
	for _, gid := range affected {
		vg = append(vg, h.estimateGroupView(d, gid))
	}
	render(c, http.StatusOK, views.EstimateReorderResult(vg))
}
