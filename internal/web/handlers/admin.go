package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/views"
)

// AdminDashboard renders the console's landing page: the headline counters and
// the signups chart at its default range.
func (h *Handlers) AdminDashboard(c *gin.Context) {
	stats, err := h.store.CountAdminStats(c.Request.Context())
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	chart, err := h.signupChart(c, c.Query("range"))
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	render(c, http.StatusOK, views.AdminDashboardPage(views.AdminDashboardData{Stats: stats, Chart: chart}))
}

// AdminSignupsChart re-renders just the chart card for the range toggle.
func (h *Handlers) AdminSignupsChart(c *gin.Context) {
	chart, err := h.signupChart(c, c.Query("range"))
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	render(c, http.StatusOK, views.SignupsChartCard(chart))
}

// signupChart resolves the requested range against the fixed set of windows and
// builds the chart geometry. Because the range is looked up rather than parsed,
// an arbitrary query value can never reach date_trunc.
func (h *Handlers) signupChart(c *gin.Context, rangeKey string) (views.SignupChart, error) {
	r := views.LookupSignupRange(rangeKey)
	buckets, err := h.store.SignupSeries(c.Request.Context(), r.Unit, r.Buckets)
	if err != nil {
		return views.SignupChart{}, err
	}
	return views.BuildSignupChart(buckets, r), nil
}

// AdminUsers renders one page of the user list.
func (h *Handlers) AdminUsers(c *gin.Context) {
	ctx := c.Request.Context()
	total, err := h.store.CountUsers(ctx)
	if err != nil {
		writeStoreErr(c, err)
		return
	}

	totalPages := (total + views.AdminUsersPageSize - 1) / views.AdminUsersPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	page := 1
	if n, err := strconv.Atoi(c.Query("page")); err == nil && n > 1 {
		page = n
	}
	if page > totalPages {
		page = totalPages
	}

	users, err := h.store.ListUsers(ctx, views.AdminUsersPageSize, (page-1)*views.AdminUsersPageSize)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	render(c, http.StatusOK, views.AdminUsersPage(views.AdminUsersData{
		Users: users, Page: page, TotalPages: totalPages, Total: total,
	}))
}

// AdminUserDetail renders one user with the actions available on them.
func (h *Handlers) AdminUserDetail(c *gin.Context) {
	h.renderAdminUser(c, http.StatusOK, "")
}

// renderAdminUser assembles and renders the user detail page, optionally with an
// error message. Shared by the detail route and by actions that fail validation,
// so a rejected action redisplays the page rather than a bare error string.
func (h *Handlers) renderAdminUser(c *gin.Context, status int, errMsg string) {
	ctx := c.Request.Context()
	id, ok := adminUserID(c)
	if !ok {
		return
	}
	user, err := h.store.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		writeStoreErr(c, err)
		return
	}

	d := views.AdminUserData{User: user, IsSelf: user.ID == currentUserID(c), Err: errMsg}
	if d.CompFlagSet, d.CompUntil, err = h.store.GetComp(ctx, id); err != nil {
		writeStoreErr(c, err)
		return
	}
	if d.CompActive, err = h.store.IsBillingExempt(ctx, id); err != nil {
		writeStoreErr(c, err)
		return
	}
	if d.StripeCustomerID, err = h.store.GetUserStripeCustomer(ctx, id); err != nil {
		writeStoreErr(c, err)
		return
	}
	if d.Subscriptions, err = h.store.ListSubscriptionsForUser(ctx, id); err != nil {
		writeStoreErr(c, err)
		return
	}
	if d.CanDelete, d.DeleteBlocked, err = h.deletable(c, user); err != nil {
		writeStoreErr(c, err)
		return
	}
	render(c, status, views.AdminUserPage(d))
}

// deletable reports whether the user may be deleted from the console, and why
// not when they may not.
//
// The rule is that we must not delete anyone Stripe might still bill: deleting
// the row here does not cancel their subscription, so an active, trialing or
// past-due subscription blocks the action until it is cancelled in Stripe. A
// user who never subscribed has nothing to bill and is deletable. An admin
// cannot delete themselves.
func (h *Handlers) deletable(c *gin.Context, user store.User) (bool, string, error) {
	if user.ID == currentUserID(c) {
		return false, "You cannot delete your own account from here.", nil
	}
	active, err := h.store.HasAccessGrantingSubscription(c.Request.Context(), user.ID)
	if err != nil {
		return false, "", err
	}
	if active {
		return false, "This user has a live subscription. Cancel it in Stripe first — deleting here does not stop their billing.", nil
	}
	return true, "", nil
}

// AdminUserDisable suspends an account.
func (h *Handlers) AdminUserDisable(c *gin.Context) {
	h.setAdminUserDisabled(c, true)
}

// AdminUserEnable lifts a suspension.
func (h *Handlers) AdminUserEnable(c *gin.Context) {
	h.setAdminUserDisabled(c, false)
}

func (h *Handlers) setAdminUserDisabled(c *gin.Context, disabled bool) {
	id, ok := adminUserID(c)
	if !ok {
		return
	}
	if disabled && id == currentUserID(c) {
		h.renderAdminUser(c, http.StatusBadRequest, "You cannot disable your own account.")
		return
	}
	if err := h.store.SetUserDisabled(c.Request.Context(), id, disabled); err != nil {
		writeStoreErr(c, err)
		return
	}
	slog.Info("admin: user access changed",
		"actor_id", currentUserID(c), "target_id", id, "disabled", disabled)
	redirectToAdminUser(c, id)
}

// AdminUserGrantComp gives the user a complimentary subscription for one of the
// fixed durations.
func (h *Handlers) AdminUserGrantComp(c *gin.Context) {
	id, ok := adminUserID(c)
	if !ok {
		return
	}
	until, ok := compUntil(c.PostForm("duration"))
	if !ok {
		h.renderAdminUser(c, http.StatusBadRequest, "Pick one of the offered comp durations.")
		return
	}
	if err := h.store.GrantComp(c.Request.Context(), id, until); err != nil {
		writeStoreErr(c, err)
		return
	}
	slog.Info("admin: comp granted",
		"actor_id", currentUserID(c), "target_id", id, "duration", c.PostForm("duration"))
	redirectToAdminUser(c, id)
}

// AdminUserRevokeComp removes a complimentary subscription.
func (h *Handlers) AdminUserRevokeComp(c *gin.Context) {
	id, ok := adminUserID(c)
	if !ok {
		return
	}
	if err := h.store.RevokeComp(c.Request.Context(), id); err != nil {
		writeStoreErr(c, err)
		return
	}
	slog.Info("admin: comp revoked", "actor_id", currentUserID(c), "target_id", id)
	redirectToAdminUser(c, id)
}

// AdminUserDelete permanently deletes a user and all their data. It re-checks
// deletability at submit time — the page may have been open while their
// subscription changed — and requires the operator to retype the user's email.
func (h *Handlers) AdminUserDelete(c *gin.Context) {
	ctx := c.Request.Context()
	id, ok := adminUserID(c)
	if !ok {
		return
	}
	user, err := h.store.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		writeStoreErr(c, err)
		return
	}

	canDelete, blocked, err := h.deletable(c, user)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	if !canDelete {
		h.renderAdminUser(c, http.StatusBadRequest, blocked)
		return
	}
	if c.PostForm("confirm_email") != user.Email {
		h.renderAdminUser(c, http.StatusBadRequest, "The confirmation email did not match.")
		return
	}
	if err := h.store.DeleteUser(ctx, id); err != nil {
		writeStoreErr(c, err)
		return
	}
	slog.Warn("admin: user deleted",
		"actor_id", currentUserID(c), "target_id", id, "target_email", user.Email)
	c.Redirect(http.StatusSeeOther, "/admin/users")
}

// compUntil maps a duration key to the comp's end time. A lifetime comp is a nil
// end time (stored as NULL), not a far-future date, so nothing has to recognise
// a sentinel later. The false return rejects anything outside the offered set.
func compUntil(key string) (*time.Time, bool) {
	now := time.Now()
	switch key {
	case "1m":
		t := now.AddDate(0, 1, 0)
		return &t, true
	case "6m":
		t := now.AddDate(0, 6, 0)
		return &t, true
	case "1y":
		t := now.AddDate(1, 0, 0)
		return &t, true
	case "lifetime":
		return nil, true
	}
	return nil, false
}

// adminUserID parses the :id path parameter, writing a 404 and reporting false
// when it is not a number.
func adminUserID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(http.StatusNotFound)
		c.Abort()
		return 0, false
	}
	return id, true
}

func redirectToAdminUser(c *gin.Context, id int64) {
	c.Redirect(http.StatusSeeOther, "/admin/users/"+strconv.FormatInt(id, 10))
}
