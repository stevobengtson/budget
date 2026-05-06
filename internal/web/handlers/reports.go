package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/web/views"
)

func (h *Handlers) ReportsSpending(c *gin.Context) {
	ctx := c.Request.Context()
	now := time.Now()
	period := c.DefaultQuery("period", "this")
	since, until := periodRange(period, now)

	rows, err := h.store.SpendingByCategory(ctx, since, until)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	var total int64
	for _, r := range rows {
		total += r.OutflowCents
	}
	render(c, http.StatusOK, views.ReportsSpendingPage(views.SpendingData{
		Rows: rows, Period: period, Since: since, Until: until, Total: total,
	}))
}

func (h *Handlers) ReportsCashflow(c *gin.Context) {
	ctx := c.Request.Context()
	rows, err := h.store.MonthlyCashflow(ctx, 12)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	render(c, http.StatusOK, views.ReportsCashflowPage(views.CashflowData{Rows: rows}))
}

func periodRange(period string, now time.Time) (time.Time, time.Time) {
	switch period {
	case "30d":
		return now.AddDate(0, 0, -29), now
	case "90d":
		return now.AddDate(0, 0, -89), now
	case "ytd":
		return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC), now
	}
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return first, first.AddDate(0, 1, -1)
}
