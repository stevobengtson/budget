package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/oidcauth"
)

// googleIdentity is a verified assertion, as the provider would give it.
func googleIdentity(subject, email string, verified bool) oidcauth.Identity {
	return oidcauth.Identity{
		Provider:      oidcauth.ProviderGoogle,
		Subject:       subject,
		Email:         email,
		Name:          "Test Person",
		EmailVerified: verified,
	}
}

// Both parties independently proved control of the same mailbox, so the
// identity is attached and the user signed in.
func TestVerifiedIdentityAutoLinksToMatchingAccount(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "link@example.com")
	m.last = mailMessageZero()

	r, err := svc.LoginWithIdentity(ctx, googleIdentity("google-sub-1", "link@example.com", true), webInfo())
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if r.NeedsChallenge() {
		t.Fatal("no second factor is configured")
	}
	if _, err := svc.AuthenticateSession(ctx, r.SessionToken); err != nil {
		t.Fatal("the issued session should authenticate")
	}

	// The link now exists...
	u, err := st.GetUserByEmail(ctx, "link@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := svc.ListIdentities(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].Subject != "google-sub-1" {
		t.Fatalf("identities = %+v, want the linked google subject", ids)
	}
	// ...and the account holder was told, because a link they did not make is
	// exactly what they would want to know about.
	if !strings.Contains(m.last.Text, "sign-in provider was linked") {
		t.Errorf("expected a link notification, got: %q", m.last.Text)
	}
}

// A second sign-in uses the existing link rather than creating another.
func TestSecondSignInReusesTheLink(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "again@example.com")

	id := googleIdentity("google-sub-2", "again@example.com", true)
	if _, err := svc.LoginWithIdentity(ctx, id, webInfo()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoginWithIdentity(ctx, id, webInfo()); err != nil {
		t.Fatalf("second sign-in: %v", err)
	}

	u, err := st.GetUserByEmail(ctx, "again@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := svc.ListIdentities(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("identities = %d, want 1", len(ids))
	}
	if ids[0].LastUsedAt == nil {
		t.Error("a sign-in should record that the identity was used")
	}
}

// Google reports email_verified=false for some Workspace configurations. An
// unverified assertion is not evidence of controlling the mailbox, so it must
// never claim an existing account.
func TestUnverifiedIdentityNeverLinks(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "unverified-idp@example.com")

	_, err := svc.LoginWithIdentity(ctx,
		googleIdentity("google-sub-3", "unverified-idp@example.com", false), webInfo())
	if err != auth.ErrIdentityUnverified {
		t.Fatalf("want ErrIdentityUnverified, got %v", err)
	}

	u, err := st.GetUserByEmail(ctx, "unverified-idp@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := svc.ListIdentities(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("nothing should have been linked, got %+v", ids)
	}
}

// The LOCAL address must be verified too. Otherwise someone who registered an
// address they do not own could have it claimed later by whoever actually does.
func TestUnverifiedLocalAccountIsNotClaimable(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()

	// Registered, never verified.
	if err := svc.Register(ctx, "never-verified@example.com", goodPassword); err != nil {
		t.Fatal(err)
	}
	m.last = mailMessageZero()

	_, err := svc.LoginWithIdentity(ctx,
		googleIdentity("google-sub-4", "never-verified@example.com", true), webInfo())
	if err != auth.ErrIdentityUnverified {
		t.Fatalf("want ErrIdentityUnverified, got %v", err)
	}
}

// No account, no silent creation: registration seeds a starter budget in a
// language the first-run wizard has yet to ask for.
func TestNoAccountIsNotSilentlyCreated(t *testing.T) {
	svc, _, st := newMFAService(t)
	ctx := context.Background()

	_, err := svc.LoginWithIdentity(ctx,
		googleIdentity("google-sub-5", "stranger@example.com", true), webInfo())
	if err != auth.ErrNoAccountForIdentity {
		t.Fatalf("want ErrNoAccountForIdentity, got %v", err)
	}
	if _, err := st.GetUserByEmail(ctx, "stranger@example.com"); err == nil {
		t.Fatal("no account should have been created")
	}
}

// THE rule that would otherwise be missed: federated sign-in is a FIRST factor.
// A user who turned on two-step verification must not find that adding "Sign in
// with Google" quietly turned it off.
func TestLinkedIdentityStillRequiresTheSecondFactor(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	uid, secret, _ := enrolTOTP(t, svc, m, "idp2fa@example.com")
	_ = uid

	r, err := svc.LoginWithIdentity(ctx,
		googleIdentity("google-sub-6", "idp2fa@example.com", true), webInfo())
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("an account with an authenticator must still be challenged")
	}
	if r.SessionToken != "" {
		t.Fatal("a challenge must never come with a session token")
	}

	code, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, code, webInfo()); err != nil {
		t.Fatalf("finishing the challenge should sign in: %v", err)
	}
}

// A suspended account cannot be reached through a provider either.
func TestDisabledAccountRefusesIdentitySignIn(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "idpdisabled@example.com")

	u, err := st.GetUserByEmail(ctx, "idpdisabled@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserDisabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoginWithIdentity(ctx,
		googleIdentity("google-sub-7", "idpdisabled@example.com", true), webInfo()); err != auth.ErrAccountDisabled {
		t.Fatalf("want ErrAccountDisabled, got %v", err)
	}
}

// An identity already attached to somebody else must not be re-pointed.
func TestIdentityCannotBeStolenFromAnotherAccount(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "owner@example.com")
	registerVerified(t, svc, m, "thief@example.com")

	if _, err := svc.LoginWithIdentity(ctx,
		googleIdentity("shared-sub", "owner@example.com", true), webInfo()); err != nil {
		t.Fatal(err)
	}
	thief, err := st.GetUserByEmail(ctx, "thief@example.com")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.LinkIdentityToUser(ctx, thief.ID,
		googleIdentity("shared-sub", "owner@example.com", true))
	if err != auth.ErrIdentityTaken {
		t.Fatalf("want ErrIdentityTaken, got %v", err)
	}
}

// Unlinking the only way in would strand the account.
func TestCannotUnlinkTheLastWayIn(t *testing.T) {
	svc, _, st := newMFAService(t)
	ctx := context.Background()

	// An account with no password at all, as a provider-only signup would be.
	uid, err := st.CreateUser(ctx, "idponly@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if err := svc.LinkIdentityToUser(ctx, uid, googleIdentity("only-sub", "idponly@example.com", true)); err != nil {
		t.Fatal(err)
	}

	ids, err := svc.ListIdentities(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UnlinkIdentity(ctx, uid, ids[0].ID); err != auth.ErrLastFactor {
		t.Fatalf("want ErrLastFactor, got %v", err)
	}

	// With a password set, unlinking is fine.
	hash, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePasswordHash(ctx, uid, hash); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnlinkIdentity(ctx, uid, ids[0].ID); err != nil {
		t.Fatalf("unlinking with a password available should work: %v", err)
	}
}

// Apple reports the email only on the first authorization. A later sign-in
// carries no address, and must not wipe the one already recorded.
func TestLaterSignInDoesNotEraseTheStoredEmail(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "apple@example.com")

	first := oidcauth.Identity{
		Provider: oidcauth.ProviderApple, Subject: "apple-sub",
		Email: "apple@example.com", EmailVerified: true,
	}
	if _, err := svc.LoginWithIdentity(ctx, first, webInfo()); err != nil {
		t.Fatal(err)
	}

	// Apple's subsequent responses carry no email at all.
	later := oidcauth.Identity{Provider: oidcauth.ProviderApple, Subject: "apple-sub"}
	if _, err := svc.LoginWithIdentity(ctx, later, webInfo()); err != nil {
		t.Fatalf("a later sign-in should still work: %v", err)
	}

	u, err := st.GetUserByEmail(ctx, "apple@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := svc.ListIdentities(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].Email != "apple@example.com" {
		t.Fatalf("the first-authorization email must survive, got %+v", ids)
	}
}
