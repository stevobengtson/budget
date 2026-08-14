package auth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/passkey"
	"github.com/sbengtson/budget/internal/core/store"
)

const (
	pkRPID    = "pigglet.ca"
	pkOrigin  = "https://pigglet.ca"
	pkTestKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// newPasskeyService builds an auth service with passkeys and TOTP available.
func newPasskeyService(t *testing.T) (*auth.Service, *capMailer, *store.Store) {
	t.Helper()
	sealer, err := crypto.NewSealer(pkTestKey)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(openTestDB(t))
	pk, err := passkey.New(st, passkey.Config{
		RPID: pkRPID, RPDisplayName: "Pigglet", Origins: []string{pkOrigin},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := &capMailer{}
	return auth.NewService(st, m, "http://localhost:8080",
		auth.Config{Sealer: sealer, Passkeys: pk}), m, st
}

// enrolPasskey registers a credential through the service. userVerified controls
// whether the virtual authenticator claims to have checked a biometric or PIN.
func enrolPasskey(t *testing.T, svc *auth.Service, userID int64, userVerified bool) (
	virtualwebauthn.RelyingParty, *virtualwebauthn.Authenticator, virtualwebauthn.Credential,
) {
	t.Helper()
	ctx := context.Background()
	rp := virtualwebauthn.RelyingParty{Name: "Pigglet", ID: pkRPID, Origin: pkOrigin}
	a := virtualwebauthn.NewAuthenticator()
	a.Options.UserNotVerified = !userVerified
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	ceremony, options, err := svc.BeginPasskeyRegistration(ctx, userID, store.SessionInfo{UserAgent: "test"})
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	opts, err := virtualwebauthn.ParseAttestationOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	a.Options.UserHandle = []byte(opts.UserID)
	response := virtualwebauthn.CreateAttestationResponse(rp, a, cred, *opts)
	if err := svc.FinishPasskeyRegistration(ctx, userID, ceremony, []byte(response), "Test key"); err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	a.AddCredential(cred)
	return rp, &a, cred
}

// signInWithPasskey runs a sign-in ceremony and returns the result.
func signInWithPasskey(t *testing.T, svc *auth.Service,
	rp virtualwebauthn.RelyingParty, a *virtualwebauthn.Authenticator, cred virtualwebauthn.Credential,
) (auth.LoginResult, error) {
	t.Helper()
	ctx := context.Background()
	ceremony, options, err := svc.BeginPasskeyLogin(ctx, webInfo())
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	opts, err := virtualwebauthn.ParseAssertionOptions(string(options))
	if err != nil {
		t.Fatal(err)
	}
	response := virtualwebauthn.CreateAssertionResponse(rp, *a, cred, *opts)
	return svc.FinishPasskeyLogin(ctx, ceremony, []byte(response), webInfo())
}

// A verified assertion is two factors in one gesture — the device, plus the
// biometric or PIN that unlocked it — so it stands alone even when the account
// has a second factor configured.
func TestVerifiedPasskeySkipsTheChallenge(t *testing.T) {
	svc, m, _ := newPasskeyService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "uv@example.com")
	tok, err := svc.Login(ctx, "uv@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	// Turn on a second factor, which a password sign-in would now have to clear.
	if _, err := svc.SetEmailOTPEnabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}

	rp, a, cred := enrolPasskey(t, svc, u.ID, true)
	r, err := signInWithPasskey(t, svc, rp, a, cred)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if r.NeedsChallenge() {
		t.Fatal("a user-verified assertion already proves two factors")
	}
	if _, err := svc.AuthenticateSession(ctx, r.SessionToken); err != nil {
		t.Fatal("the issued session should authenticate")
	}
}

// An UNVERIFIED assertion proves possession only — someone holding the device,
// nothing more — so the account's second factor still applies. Getting this
// backwards would silently downgrade every account that asked for 2FA.
func TestUnverifiedPasskeyStillRequiresTheSecondFactor(t *testing.T) {
	svc, m, _ := newPasskeyService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "nouv@example.com")
	tok, err := svc.Login(ctx, "nouv@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEmailOTPEnabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}

	rp, a, cred := enrolPasskey(t, svc, u.ID, false)
	r, err := signInWithPasskey(t, svc, rp, a, cred)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("an unverified assertion is one factor; the account's second factor still applies")
	}
	if r.SessionToken != "" {
		t.Fatal("a challenge must never come with a session token")
	}
}

// With no second factor configured, even an unverified assertion signs in —
// there is nothing further to prove.
func TestUnverifiedPasskeySignsInWhenNoSecondFactor(t *testing.T) {
	svc, m, _ := newPasskeyService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "plainpk@example.com")
	tok, err := svc.Login(ctx, "plainpk@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}

	rp, a, cred := enrolPasskey(t, svc, u.ID, false)
	r, err := signInWithPasskey(t, svc, rp, a, cred)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if r.NeedsChallenge() {
		t.Fatal("nothing else is configured, so there is nothing to challenge for")
	}
}

// Removing the last passkey from an account with no password would leave
// nothing that can sign in.
func TestCannotRemoveTheLastPasskeyWithoutAPassword(t *testing.T) {
	svc, _, st := newPasskeyService(t)
	ctx := context.Background()

	uid, err := st.CreateUser(ctx, "nopassword@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	enrolPasskey(t, svc, uid, true)

	creds, err := svc.ListPasskeys(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeletePasskey(ctx, uid, creds[0].ID); err != auth.ErrLastFactor {
		t.Fatalf("want ErrLastFactor, got %v", err)
	}

	// With a second passkey, removing one is fine.
	enrolPasskey(t, svc, uid, true)
	if err := svc.DeletePasskey(ctx, uid, creds[0].ID); err != nil {
		t.Fatalf("removing one of two should be allowed: %v", err)
	}
}

// A passkey is an answerable step-up prompt for someone with no password.
func TestPasskeyAppearsInStepUpFactors(t *testing.T) {
	svc, m, _ := newPasskeyService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "stepuppk@example.com")
	tok, err := svc.Login(ctx, "stepuppk@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	enrolPasskey(t, svc, u.ID, true)

	factors, err := svc.AvailableStepUpFactors(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range factors {
		if f == auth.FactorPasskey {
			found = true
		}
	}
	if !found {
		t.Fatalf("factors = %v, want one of them to be the passkey", factors)
	}
}

// Enrolling notifies the account holder, like every other security change.
func TestEnrollingAPasskeyNotifies(t *testing.T) {
	svc, m, _ := newPasskeyService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "notifypk@example.com")
	tok, err := svc.Login(ctx, "notifypk@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	enrolPasskey(t, svc, u.ID, true)

	if !strings.Contains(m.last.Text, "passkey was added") {
		t.Errorf("expected a passkey-added notice, got: %q", m.last.Text)
	}
}
