package plaid

import (
	"context"
	"errors"
	"strings"
	"testing"

	plaidapi "github.com/plaid/plaid-go/v45/plaid"
)

// leakyErr mimics an SDK error whose text embeds sensitive request context.
type leakyErr struct{ code string }

func (e leakyErr) Error() string {
	return "request failed: public_token=public-sandbox-SECRET access_token=access-sandbox-SECRET"
}
func (e leakyErr) PlaidErrorCode() string { return e.code }

type leakyAPI struct{ fakeAPI }

func (l *leakyAPI) ItemPublicTokenExchange(context.Context, string) (plaidapi.ItemPublicTokenExchangeResponse, error) {
	return plaidapi.ItemPublicTokenExchangeResponse{}, leakyErr{code: "INVALID_PUBLIC_TOKEN"}
}
func (l *leakyAPI) LinkTokenCreate(context.Context, plaidapi.LinkTokenCreateRequest) (plaidapi.LinkTokenCreateResponse, error) {
	return plaidapi.LinkTokenCreateResponse{}, leakyErr{code: "INVALID_REQUEST"}
}

// TestErrorsCarryNoTokenMaterial guards the no-token-leakage rule: errors
// returned from Plaid calls surface only the machine-readable error code,
// never the SDK error text (which can embed tokens and request bodies).
func TestErrorsCarryNoTokenMaterial(t *testing.T) {
	s, _, uid := newTestService(t, &leakyAPI{})
	ctx := context.Background()

	_, err := s.ExchangePublicToken(ctx, uid, "public-sandbox-SECRET", "ins", "Bank")
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); strings.Contains(msg, "SECRET") {
		t.Errorf("exchange error leaks token material: %q", msg)
	}
	if !strings.Contains(err.Error(), "INVALID_PUBLIC_TOKEN") {
		t.Errorf("exchange error lost the loggable code: %q", err.Error())
	}

	_, err = s.CreateLinkToken(ctx, uid, "en-CA", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); strings.Contains(msg, "SECRET") {
		t.Errorf("link token error leaks material: %q", msg)
	}
}

// TestUnconfiguredServiceIsInert: every entry point of an unconfigured service
// declines with ErrNotConfigured instead of panicking on nil internals.
func TestUnconfiguredServiceIsInert(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	if s.Enabled() {
		t.Fatal("zero service reports enabled")
	}
	if _, err := s.CreateLinkToken(ctx, 1, "en-CA", ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("CreateLinkToken err = %v", err)
	}
	if _, err := s.ExchangePublicToken(ctx, 1, "pt", "", ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("ExchangePublicToken err = %v", err)
	}
	if err := s.HandleWebhook(ctx, nil, "", nil); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("HandleWebhook err = %v", err)
	}
	s.RemoveAllItemsForUser(ctx, 1) // must not panic
}
