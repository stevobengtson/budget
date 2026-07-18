package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// monthOrNow returns the ?month= query value or the current month key.
func monthOrNow(c *gin.Context) string {
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}
	return month
}

// BudgetIndex renders the budget tab for the requested month (default: current).
func (h *Handlers) BudgetIndex(c *gin.Context) {
	ctx := c.Request.Context()

	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	collapsed := sidebarCollapsed(c)

	data, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, "month budget: %v", err)
		return
	}
	_ = rows
	render(c, http.StatusOK, views.BudgetPage(data, collapsed))
}

// budgetData loads everything needed to render the budget page for the
// requested month and returns the rendered view-model plus the flat row
// slice (callers that need to find a single row by ID can reuse it without
// a second store round-trip).
func (h *Handlers) budgetData(ctx context.Context, month string) (views.BudgetData, []store.CategoryBudget, error) {
	rows, err := h.store.MonthBudget(ctx, month)
	if err != nil {
		return views.BudgetData{}, nil, fmt.Errorf("month budget: %w", err)
	}
	groups, err := h.buildGroups(ctx, rows)
	if err != nil {
		return views.BudgetData{}, nil, fmt.Errorf("build groups: %w", err)
	}

	incTotal, _ := h.store.TotalIncome(ctx, month)
	uncat, _ := h.store.UncategorizedSpent(ctx, month)
	credit, _ := h.store.CreditCardActivityForMonth(ctx, month)
	incomeRows, _ := h.store.ListIncomes(ctx, month)

	accts, err := h.store.ListAccounts(ctx, false) // exclude archived
	if err != nil {
		return views.BudgetData{}, nil, fmt.Errorf("list accounts: %w", err)
	}

	creditFiltered := credit[:0]
	for _, ca := range credit {
		if ca.PurchasesCents != 0 || ca.PaymentsCents != 0 {
			creditFiltered = append(creditFiltered, ca)
		}
	}

	var assigned int64
	for _, r := range rows {
		if !r.IsIncome {
			assigned += r.AssignedCents
		}
	}

	prev := store.PrevMonth(month)
	t, _ := time.Parse("2006-01", month)
	next := t.AddDate(0, 1, 0).Format("2006-01")

	return views.BudgetData{
		Month:              month,
		PrevMonth:          prev,
		NextMonth:          next,
		Estimated:          incTotal,
		Budgeted:           assigned,
		Remain:             incTotal - assigned,
		CreditRows:         creditFiltered,
		Groups:             groups,
		IncomeRows:         incomeRows,
		Accounts:           accts,
		UncategorizedSpent: uncat,
	}, rows, nil
}

// groupName returns a group's name, or "" if not found.
func (h *Handlers) groupName(ctx context.Context, gid int64) string {
	groups, err := h.store.ListGroups(ctx)
	if err != nil {
		return ""
	}
	for _, g := range groups {
		if g.ID == gid {
			return g.Name
		}
	}
	return ""
}

// buildGroups lists every user category group and bins the month's category
// rows into it, preserving group sort order. The system "Income" group (the one
// that owns the is_income category) is excluded — its category is surfaced in
// the banner and the income section, not the budget table. Empty user groups
// are kept so the user can add the first category to them.
func (h *Handlers) buildGroups(ctx context.Context, rows []store.CategoryBudget) ([]views.BudgetGroup, error) {
	incomeGroupID := int64(-1)
	byGroup := make(map[int64][]store.CategoryBudget)
	for _, r := range rows {
		if r.IsIncome {
			incomeGroupID = r.GroupID
			continue
		}
		byGroup[r.GroupID] = append(byGroup[r.GroupID], r)
	}

	groups, err := h.store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]views.BudgetGroup, 0, len(groups))
	for _, g := range groups {
		if g.ID == incomeGroupID {
			continue
		}
		out = append(out, views.BudgetGroup{ID: g.ID, Name: g.Name, Rows: byGroup[g.ID]})
	}
	return out, nil
}

// BudgetAssign updates the assigned amount for a category and returns
// the swapped row partial plus out-of-band updates for the banner stats
// that move when an assignment changes (Budgeted, Remain).
func (h *Handlers) BudgetAssign(c *gin.Context) {
	ctx := c.Request.Context()
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	cents, err := money.Parse(c.PostForm("amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "invalid amount: %v", err)
		return
	}
	if err := h.store.SetAssigned(ctx, month, catID, cents); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	data, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetAssignResult(month, findCatRow(rows, catID), data))
}

// findCatRow returns the budget row for the given category from a flat
// MonthBudget slice, so a single-row swap can render just the edited category
// without a second store round-trip. Returns a zero row if not found.
func findCatRow(rows []store.CategoryBudget, catID int64) store.CategoryBudget {
	for _, r := range rows {
		if r.CategoryID == catID {
			return r
		}
	}
	return store.CategoryBudget{}
}

// BudgetAssignCopyPrev replaces the current month's assignment for a
// single category with whatever was assigned in the previous month
// (defaults to 0 if there was no entry there). Returns the same region
// fragment used by BudgetAssign so the banner + totals stay in sync.
func (h *Handlers) BudgetAssignCopyPrev(c *gin.Context) {
	ctx := c.Request.Context()
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	month := c.Query("month")
	if month == "" {
		month = store.MonthKey(time.Now())
	}

	prev := store.PrevMonth(month)
	prevCents, err := h.store.GetAssigned(ctx, prev, catID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SetAssigned(ctx, month, catID, prevCents); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	data, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetAssignResult(month, findCatRow(rows, catID), data))
}

// BudgetSetRollover updates a category's rollover mode and returns the same
// region fragment as BudgetAssign so Available/totals/banner stay in sync.
func (h *Handlers) BudgetSetRollover(c *gin.Context) {
	ctx := c.Request.Context()
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	month := monthOrNow(c)

	mode := c.PostForm("mode")
	switch mode {
	case store.RolloverNone, store.RolloverCarry, store.RolloverCarryPositive:
	default:
		c.String(http.StatusBadRequest, "invalid rollover mode: %q", mode)
		return
	}
	if err := h.store.SetRolloverMode(ctx, catID, mode); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	data, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetAssignResult(month, findCatRow(rows, catID), data))
}

// BudgetGoalEdit returns the expanding goal editor row for a category.
func (h *Handlers) BudgetGoalEdit(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	_, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetGoalEditor(month, findCatRow(rows, catID)))
}

// BudgetGoal saves (or clears) a category's goal and refreshes its row.
// Blank Goal $ clears goal_cents; blank Goal due clears goal_due_date.
func (h *Handlers) BudgetGoal(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)

	cur, err := h.findCategory(ctx, catID)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	cur.GoalCents = nil
	if v := strings.TrimSpace(c.PostForm("goal_cents")); v != "" {
		cents, err := money.Parse(v)
		if err != nil {
			c.String(http.StatusBadRequest, "goal: %v", err)
			return
		}
		cur.GoalCents = &cents
	}
	cur.GoalDueDate = nil
	if v := strings.TrimSpace(c.PostForm("goal_due")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			c.String(http.StatusBadRequest, "goal due: %v", err)
			return
		}
		cur.GoalDueDate = &t
	}
	if err := h.store.UpdateCategory(ctx, *cur); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	_, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetGoalResult(month, findCatRow(rows, catID)))
}

// BudgetGroupNew returns the transient editable group row appended to the table.
func (h *Handlers) BudgetGroupNew(c *gin.Context) {
	render(c, http.StatusOK, views.BudgetGroupNewRow())
}

// BudgetGroupCreate creates a group at the end and returns its real <tbody>.
func (h *Handlers) BudgetGroupCreate(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	max, err := h.store.MaxGroupSortOrder(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	id, err := h.store.CreateGroup(ctx, name, max+1)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetGroupTbody(month, views.BudgetGroup{ID: id, Name: name}))
}

// BudgetGroupRename updates a group's name and returns the refreshed header row.
func (h *Handlers) BudgetGroupRename(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}

	groups, err := h.store.ListGroups(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var cur *store.CategoryGroup
	for i := range groups {
		if groups[i].ID == gid {
			cur = &groups[i]
			break
		}
	}
	if cur == nil {
		c.String(http.StatusNotFound, "group not found")
		return
	}
	if err := h.store.UpdateGroup(ctx, store.CategoryGroup{ID: gid, Name: name, SortOrder: cur.SortOrder}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// Rebuild the group (with rows) so the header knows whether to show the
	// "delete empty group" control.
	_, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	g := views.BudgetGroup{ID: gid, Name: name}
	for _, r := range rows {
		if !r.IsIncome && r.GroupID == gid {
			g.Rows = append(g.Rows, r)
		}
	}
	render(c, http.StatusOK, views.BudgetGroupHeader(month, g))
}

// BudgetGroupDelete removes an empty group and re-renders the region.
func (h *Handlers) BudgetGroupDelete(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)

	// DeleteGroup enforces emptiness (including cleaning up the group's archived
	// categories, and rejecting any that still carry transaction history).
	if err := h.store.DeleteGroup(ctx, gid); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetRegion(data))
}

// BudgetCategoryNew returns the transient editable category row for a group.
func (h *Handlers) BudgetCategoryNew(c *gin.Context) {
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	render(c, http.StatusOK, views.BudgetCategoryNewRow(monthOrNow(c), gid))
}

// BudgetCategoryCreate appends a category to a group and returns its row.
func (h *Handlers) BudgetCategoryCreate(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	gid, _ := strconv.ParseInt(c.Param("gid"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	max, err := h.store.MaxCategorySortOrder(ctx, gid)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	id, err := h.store.CreateCategory(ctx, store.Category{GroupID: gid, Name: name, SortOrder: max + 1})
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// A brand-new category has zero assigned/spent/available and no goal.
	row := store.CategoryBudget{CategoryID: id, GroupID: gid, CategoryName: name}

	// Refresh the group header out of band: the group is no longer empty, so its
	// "delete empty group" control must disappear. Rows need only be non-empty
	// for the header to hide that control.
	g := views.BudgetGroup{ID: gid, Name: h.groupName(ctx, gid), Rows: []store.CategoryBudget{row}}
	render(c, http.StatusOK, views.BudgetCategoryCreateResult(month, row, g))
}

// BudgetCategoryRename updates a category's name and returns its refreshed row.
func (h *Handlers) BudgetCategoryRename(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "name required")
		return
	}
	cur, err := h.findCategory(ctx, catID)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	cur.Name = name
	if err := h.store.UpdateCategory(ctx, *cur); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	_, rows, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetRow(month, findCatRow(rows, catID)))
}

// BudgetCategoryArchive archives a category and re-renders the region (totals
// and banner can move when a category with spending is removed).
func (h *Handlers) BudgetCategoryArchive(c *gin.Context) {
	ctx := c.Request.Context()
	month := monthOrNow(c)
	catID, _ := strconv.ParseInt(c.Param("catID"), 10, 64)
	if err := h.store.ArchiveCategory(ctx, catID); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	data, _, err := h.budgetData(ctx, month)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.BudgetRegion(data))
}

// findCategory loads one active category by id, rejecting the system Income
// category (which must not be renamed or re-goaled from the budget page).
func (h *Handlers) findCategory(ctx context.Context, id int64) (*store.Category, error) {
	cats, err := h.store.ListCategories(ctx, false)
	if err != nil {
		return nil, err
	}
	for i := range cats {
		if cats[i].ID == id {
			if cats[i].IsIncome {
				return nil, fmt.Errorf("the Income category is system-managed")
			}
			return &cats[i], nil
		}
	}
	return nil, fmt.Errorf("category not found")
}
