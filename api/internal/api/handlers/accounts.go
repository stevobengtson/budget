// Package handlers implements the JSON HTTP handlers for the API. Every handler
// scopes data to the authenticated user via store.For(userID), where userID is
// the JWT `sub` claim placed in the context by the auth middleware.
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/api/middleware"
	"github.com/sbengtson/budget/internal/core/store"
)

// Handlers carries shared dependencies for the API handlers.
type Handlers struct {
	store *store.Store
}

// New constructs Handlers bound to the store.
func New(s *store.Store) *Handlers { return &Handlers{store: s} }

// accountDTO is the JSON shape returned to clients (camelCase, cents as ints).
type accountDTO struct {
	ID                   int64      `json:"id"`
	Name                 string     `json:"name"`
	Type                 string     `json:"type"`
	StartingBalanceCents int64      `json:"startingBalanceCents"`
	CreditLimitCents     *int64     `json:"creditLimitCents,omitempty"`
	AprBps               *int64     `json:"aprBps,omitempty"`
	MonthlyPaymentCents  *int64     `json:"monthlyPaymentCents,omitempty"`
	IncludeInPaydown     bool       `json:"includeInPaydown"`
	PaymentCategoryID    *int64     `json:"paymentCategoryId,omitempty"`
	ArchivedAt           *time.Time `json:"archivedAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	BalanceCents         int64      `json:"balanceCents"`
}

func dtoFromBalance(a store.AccountWithBalance) accountDTO {
	return accountDTO{
		ID:                   a.ID,
		Name:                 a.Name,
		Type:                 string(a.Type),
		StartingBalanceCents: a.StartingBalanceCents,
		CreditLimitCents:     a.CreditLimitCents,
		AprBps:               a.AprBps,
		MonthlyPaymentCents:  a.MonthlyPaymentCents,
		IncludeInPaydown:     a.IncludeInPaydown,
		PaymentCategoryID:    a.PaymentCategoryID,
		ArchivedAt:           a.ArchivedAt,
		CreatedAt:            a.CreatedAt,
		BalanceCents:         a.BalanceCents,
	}
}

// ListAccounts: GET /api/v1/accounts[?includeArchived=true]
func (h *Handlers) ListAccounts(c *gin.Context) {
	us := h.store.For(middleware.UserID(c))
	includeArchived := c.Query("includeArchived") == "true"

	accts, err := us.ListAccounts(c.Request.Context(), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list accounts"})
		return
	}

	out := make([]accountDTO, 0, len(accts))
	for _, a := range accts {
		out = append(out, dtoFromBalance(a))
	}
	c.JSON(http.StatusOK, gin.H{"accounts": out})
}

type createAccountReq struct {
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	StartingBalanceCents int64  `json:"startingBalanceCents"`
	CreditLimitCents     *int64 `json:"creditLimitCents"`
	AprBps               *int64 `json:"aprBps"`
	MonthlyPaymentCents  *int64 `json:"monthlyPaymentCents"`
	IncludeInPaydown     bool   `json:"includeInPaydown"`
	PaymentCategoryID    *int64 `json:"paymentCategoryId"`
}

// CreateAccount: POST /api/v1/accounts
func (h *Handlers) CreateAccount(c *gin.Context) {
	var req createAccountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if !validAccountType(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account type"})
		return
	}

	us := h.store.For(middleware.UserID(c))
	id, err := us.CreateAccount(c.Request.Context(), store.Account{
		Name:                 req.Name,
		Type:                 store.AccountType(req.Type),
		StartingBalanceCents: req.StartingBalanceCents,
		CreditLimitCents:     req.CreditLimitCents,
		AprBps:               req.AprBps,
		MonthlyPaymentCents:  req.MonthlyPaymentCents,
		IncludeInPaydown:     req.IncludeInPaydown,
		PaymentCategoryID:    req.PaymentCategoryID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	acct, err := us.GetAccount(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "account created but could not be loaded"})
		return
	}

	// A brand-new account has no transactions, so balance == starting balance.
	dto := accountDTO{
		ID:                   acct.ID,
		Name:                 acct.Name,
		Type:                 string(acct.Type),
		StartingBalanceCents: acct.StartingBalanceCents,
		CreditLimitCents:     acct.CreditLimitCents,
		AprBps:               acct.AprBps,
		MonthlyPaymentCents:  acct.MonthlyPaymentCents,
		IncludeInPaydown:     acct.IncludeInPaydown,
		PaymentCategoryID:    acct.PaymentCategoryID,
		ArchivedAt:           acct.ArchivedAt,
		CreatedAt:            acct.CreatedAt,
		BalanceCents:         acct.StartingBalanceCents,
	}
	c.JSON(http.StatusCreated, gin.H{"account": dto})
}

func validAccountType(t string) bool {
	for _, at := range store.AllAccountTypes() {
		if string(at) == t {
			return true
		}
	}
	return false
}
