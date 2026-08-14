// Package plaid wraps the Plaid API for the bank_sync add-on: linking bank
// accounts through Plaid Link, exchanging and encrypting access tokens, and
// syncing transactions into the store. Mirrors the billing package's
// discipline: an unconfigured environment yields an inert service (Enabled
// reports false) so the app runs without Plaid, and access tokens are
// decrypted only inside this package — never logged, never in errors.
package plaid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	plaidapi "github.com/plaid/plaid-go/v45/plaid"

	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/store"
)

// ErrNotConfigured is returned by operations that need Plaid when the client
// credentials or encryption key are unset (bank sync disabled for this env).
var ErrNotConfigured = errors.New("plaid is not configured")

// api is the slice of the Plaid API this app uses, kept narrow so tests can
// script it. The real implementation is apiClient below.
type api interface {
	LinkTokenCreate(ctx context.Context, req plaidapi.LinkTokenCreateRequest) (plaidapi.LinkTokenCreateResponse, error)
	ItemPublicTokenExchange(ctx context.Context, publicToken string) (plaidapi.ItemPublicTokenExchangeResponse, error)
	AccountsGet(ctx context.Context, accessToken string) (plaidapi.AccountsGetResponse, error)
	ItemRemove(ctx context.Context, accessToken string) error
	TransactionsSync(ctx context.Context, req plaidapi.TransactionsSyncRequest) (plaidapi.TransactionsSyncResponse, error)
	WebhookVerificationKeyGet(ctx context.Context, kid string) (plaidapi.WebhookVerificationKeyGetResponse, error)
}

// Service is the app-facing Plaid integration.
type Service struct {
	api        api
	store      *store.Store
	sealer     *crypto.Sealer
	webhookURL string
	days       int
	production bool
	configured bool
}

// CurrencyAllowed reports whether a bank account in this currency may be
// linked. The app is single-currency (CAD), so production blocks everything
// else; sandbox allows any currency because Plaid's test institutions report
// USD.
func (s *Service) CurrencyAllowed(currency string) bool {
	return !s.production || currency == "CAD"
}

// NewService builds the service from config. Missing credentials or a missing/
// invalid encryption key make it inert rather than failing startup; the
// returned error is only ever a warning-worthy explanation of why it is inert.
func NewService(st *store.Store, cfg config.Config) (*Service, error) {
	s := &Service{
		store:      st,
		webhookURL: cfg.Web.BaseURL + "/webhooks/plaid",
		days:       cfg.Plaid.DaysRequested,
	}
	if cfg.Plaid.ClientID == "" || cfg.Plaid.SecretKey == "" {
		return s, nil
	}
	sealer, err := crypto.NewSealer(cfg.Secrets.EncryptionKey)
	if err != nil {
		return s, fmt.Errorf("secrets.encryption_key: %w", err)
	}

	apiCfg := plaidapi.NewConfiguration()
	apiCfg.AddDefaultHeader("PLAID-CLIENT-ID", cfg.Plaid.ClientID)
	apiCfg.AddDefaultHeader("PLAID-SECRET", cfg.Plaid.SecretKey)
	env := plaidapi.Sandbox
	if cfg.Plaid.Environment == "production" {
		env = plaidapi.Production
		s.production = true
	}
	apiCfg.UseEnvironment(env)

	s.api = &apiClient{client: plaidapi.NewAPIClient(apiCfg)}
	s.sealer = sealer
	s.configured = true
	return s, nil
}

// Enabled reports whether Plaid is configured (credentials + encryption key).
func (s *Service) Enabled() bool { return s.configured }

// apiClient adapts the generated plaid-go request builders to the narrow api
// interface.
type apiClient struct {
	client *plaidapi.APIClient
}

func (c *apiClient) LinkTokenCreate(ctx context.Context, req plaidapi.LinkTokenCreateRequest) (plaidapi.LinkTokenCreateResponse, error) {
	resp, _, err := c.client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(req).Execute()
	return resp, err
}

func (c *apiClient) ItemPublicTokenExchange(ctx context.Context, publicToken string) (plaidapi.ItemPublicTokenExchangeResponse, error) {
	req := plaidapi.NewItemPublicTokenExchangeRequest(publicToken)
	resp, _, err := c.client.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(*req).Execute()
	return resp, err
}

func (c *apiClient) AccountsGet(ctx context.Context, accessToken string) (plaidapi.AccountsGetResponse, error) {
	req := plaidapi.NewAccountsGetRequest(accessToken)
	resp, _, err := c.client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*req).Execute()
	return resp, err
}

func (c *apiClient) ItemRemove(ctx context.Context, accessToken string) error {
	req := plaidapi.NewItemRemoveRequest(accessToken)
	_, _, err := c.client.PlaidApi.ItemRemove(ctx).ItemRemoveRequest(*req).Execute()
	return err
}

func (c *apiClient) TransactionsSync(ctx context.Context, req plaidapi.TransactionsSyncRequest) (plaidapi.TransactionsSyncResponse, error) {
	resp, _, err := c.client.PlaidApi.TransactionsSync(ctx).TransactionsSyncRequest(req).Execute()
	return resp, err
}

func (c *apiClient) WebhookVerificationKeyGet(ctx context.Context, kid string) (plaidapi.WebhookVerificationKeyGetResponse, error) {
	req := plaidapi.NewWebhookVerificationKeyGetRequest(kid)
	resp, _, err := c.client.PlaidApi.WebhookVerificationKeyGet(ctx).WebhookVerificationKeyGetRequest(*req).Execute()
	return resp, err
}

// logSyncWarn logs a warning about an item using only loggable fields: the
// item's database id and Plaid's machine-readable error code. Never pass raw
// errors or tokens here.
func logSyncWarn(msg string, itemID int64, code string) {
	slog.Warn("plaid: "+msg, "item_id", itemID, "plaid_error_code", code)
}

// plaidCoder lets a non-SDK error (e.g. a test fake) carry a Plaid error code.
type plaidCoder interface{ PlaidErrorCode() string }

// errorCode extracts Plaid's machine-readable error_code (e.g.
// ITEM_LOGIN_REQUIRED) from a plaid-go error, or "" for non-Plaid errors. The
// returned code is safe to log; the raw error may embed request context and is
// not.
func errorCode(err error) string {
	var c plaidCoder
	if errors.As(err, &c) {
		return c.PlaidErrorCode()
	}
	var apiErr plaidapi.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		var plaidErr plaidapi.PlaidError
		if e, convErr := plaidapi.ToPlaidError(apiErr); convErr == nil {
			plaidErr = e
		}
		return plaidErr.ErrorCode
	}
	return ""
}
