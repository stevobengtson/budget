package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/settings"
	"github.com/sbengtson/budget/internal/web/views"
)

func (h *Handlers) SettingsIndex(c *gin.Context) {
	ctx := c.Request.Context()
	accts, _ := h.store.ListAccounts(ctx, false)
	cats, _ := h.store.ListCategories(ctx, false)

	d := views.SettingsData{Accounts: accts, Categories: cats}
	if v, ok, _ := h.store.GetSetting(ctx, settings.DefaultAccountKey); ok && v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			d.DefaultAccountID = &id
		}
	}
	if v, ok, _ := h.store.GetSetting(ctx, settings.DefaultCategoryKey); ok && v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			d.DefaultCategoryID = &id
		}
	}
	render(c, http.StatusOK, views.SettingsPage(d))
}

func (h *Handlers) SettingsUpdate(c *gin.Context) {
	ctx := c.Request.Context()

	if err := upsertOrDeleteIDSetting(c, h, settings.DefaultAccountKey, "default_account_id"); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	if err := upsertOrDeleteIDSetting(c, h, settings.DefaultCategoryKey, "default_category_id"); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	accts, _ := h.store.ListAccounts(ctx, false)
	cats, _ := h.store.ListCategories(ctx, false)
	d := views.SettingsData{Accounts: accts, Categories: cats, Flash: "Saved."}
	if v, ok, _ := h.store.GetSetting(ctx, settings.DefaultAccountKey); ok && v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			d.DefaultAccountID = &id
		}
	}
	if v, ok, _ := h.store.GetSetting(ctx, settings.DefaultCategoryKey); ok && v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			d.DefaultCategoryID = &id
		}
	}
	render(c, http.StatusOK, views.SettingsPage(d))
}

func upsertOrDeleteIDSetting(c *gin.Context, h *Handlers, key, formField string) error {
	v := c.PostForm(formField)
	if v == "" {
		return h.store.DeleteSetting(c.Request.Context(), key)
	}
	if _, err := strconv.ParseInt(v, 10, 64); err != nil {
		return err
	}
	return h.store.SetSetting(c.Request.Context(), key, v)
}
