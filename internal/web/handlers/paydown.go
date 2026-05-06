package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/paydown"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/web/views"
)

func (h *Handlers) PaydownIndex(c *gin.Context) {
	ctx := c.Request.Context()
	horizon, _ := strconv.Atoi(c.Query("horizon"))
	if horizon < 12 || horizon > 360 {
		horizon = 60
	}

	all, _ := h.store.ListAccounts(ctx, false)
	included := make([]store.AccountWithBalance, 0)
	for _, a := range all {
		if a.IncludeInPaydown && a.AprBps != nil {
			included = append(included, a)
		}
	}
	plans := make([]paydown.Plan, 0, len(included))
	now := time.Now()
	var totalMonthly, totalInterest int64
	for _, a := range included {
		startCents := debtCents(a)
		var fallback int64
		if a.MonthlyPaymentCents != nil {
			fallback = *a.MonthlyPaymentCents
		}
		ms, _ := h.store.PaymentScheduleForCategory(ctx, a.PaymentCategoryID, now, horizon, fallback)
		schedule := make([]paydown.MonthPayment, len(ms))
		for i, m := range ms {
			schedule[i] = paydown.MonthPayment{Cents: m.Cents, Source: convSrc(m.Source)}
		}
		p, err := paydown.Compute(a.ID, a.Name, *a.AprBps, startCents, schedule, now)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		plans = append(plans, p)
		totalMonthly += p.PaymentCents
		totalInterest += p.TotalInterestCents
	}
	cats, _ := h.store.ListCategories(ctx, false)
	render(c, http.StatusOK, views.PaydownPage(views.PaydownData{
		Plans:         plans,
		Accounts:      included,
		AllAccounts:   all,
		Categories:    cats,
		Horizon:       horizon,
		TotalMonthly:  totalMonthly,
		TotalInterest: totalInterest,
	}))
}

// PaydownPaymentForm renders the modal contents for editing the
// fallback monthly payment on a paydown account.
func (h *Handlers) PaydownPaymentForm(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("acctID"), 10, 64)
	a, err := h.store.GetAccount(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	render(c, http.StatusOK, views.PaymentModal(store.AccountWithBalance{Account: a}))
}

// PaydownCategoryForm renders the modal contents for linking a budget
// category to a paydown account.
func (h *Handlers) PaydownCategoryForm(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("acctID"), 10, 64)
	a, err := h.store.GetAccount(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	cats, _ := h.store.ListCategories(ctx, false)
	groups, _ := h.store.ListGroups(ctx)
	render(c, http.StatusOK, views.CategoryModal(store.AccountWithBalance{Account: a}, cats, groups))
}

func (h *Handlers) PaydownInclude(c *gin.Context) { h.toggleInclude(c, true) }
func (h *Handlers) PaydownExclude(c *gin.Context) { h.toggleInclude(c, false) }
func (h *Handlers) toggleInclude(c *gin.Context, include bool) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("acctID"), 10, 64)
	a, err := h.store.GetAccount(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	a.IncludeInPaydown = include
	if err := h.store.UpdateAccount(ctx, a); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/paydown")
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) PaydownSetPayment(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("acctID"), 10, 64)
	a, err := h.store.GetAccount(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	if v := c.PostForm("amount"); v != "" {
		cents, err := money.Parse(v)
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		a.MonthlyPaymentCents = &cents
	} else {
		a.MonthlyPaymentCents = nil
	}
	if err := h.store.UpdateAccount(ctx, a); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/paydown")
	c.Writer.WriteHeader(http.StatusOK)
}

func (h *Handlers) PaydownSetCategory(c *gin.Context) {
	ctx := c.Request.Context()
	id, _ := strconv.ParseInt(c.Param("acctID"), 10, 64)
	a, err := h.store.GetAccount(ctx, id)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	if v := c.PostForm("category_id"); v != "" {
		if cid, err := strconv.ParseInt(v, 10, 64); err == nil {
			a.PaymentCategoryID = &cid
		}
	} else {
		a.PaymentCategoryID = nil
	}
	if err := h.store.UpdateAccount(ctx, a); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("HX-Redirect", "/paydown")
	c.Writer.WriteHeader(http.StatusOK)
}

func debtCents(a store.AccountWithBalance) int64 {
	if a.Type.IsLiability() && a.BalanceCents < 0 {
		return -a.BalanceCents
	}
	if a.BalanceCents > 0 {
		return a.BalanceCents
	}
	return 0
}

func convSrc(s store.PaymentSource) paydown.PaymentSource {
	switch s {
	case store.PaymentSpent:
		return paydown.SourceSpent
	case store.PaymentAssigned:
		return paydown.SourceAssigned
	}
	return paydown.SourceDefault
}
