package auth_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/sbengtson/budget/internal/core/auth"
)

// linkParts pulls the challenge token and code out of the emailed URL.
func linkParts(t *testing.T, body string) (challenge, code string) {
	t.Helper()
	i := strings.Index(body, "/login/magic?")
	if i < 0 {
		t.Fatalf("no magic link in body: %q", body)
	}
	rest := body[i:]
	if j := strings.IndexAny(rest, " \n\t\"<"); j >= 0 {
		rest = rest[:j]
	}
	u, err := url.Parse(rest)
	if err != nil {
		t.Fatalf("unparseable link %q: %v", rest, err)
	}
	q := u.Query()
	if q.Get("c") == "" || q.Get("k") == "" {
		t.Fatalf("link is missing its parts: %q", rest)
	}
	return q.Get("c"), q.Get("k")
}

func TestMagicLinkSignsInAndIsSingleUse(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "magic@example.com")

	if err := svc.RequestMagicLink(ctx, "magic@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	challenge, code := linkParts(t, m.last.Text)

	r, err := svc.ConfirmMagicLink(ctx, challenge, code, webInfo())
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if r.NeedsChallenge() {
		t.Fatal("no second factor is configured")
	}
	if _, err := svc.AuthenticateSession(ctx, r.SessionToken); err != nil {
		t.Fatal("the issued session should authenticate")
	}

	// A link left in an inbox must not keep working.
	if _, err := svc.ConfirmMagicLink(ctx, challenge, code, webInfo()); err == nil {
		t.Fatal("a magic link must be single-use")
	}
}

// Anti-enumeration: an unknown address must be indistinguishable from a known
// one, or this endpoint becomes a way to test which addresses have accounts.
func TestMagicLinkForUnknownAddressLooksIdentical(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()

	if err := svc.RequestMagicLink(ctx, "nobody@example.com", webInfo()); err != nil {
		t.Fatalf("an unknown address must not error: %v", err)
	}
	if m.last.Subject != "" {
		t.Fatalf("nothing should have been sent, got %q", m.last.Subject)
	}
}

// An address nobody has proved they own must not receive a working sign-in
// link — that would be a way around email verification entirely.
func TestMagicLinkRefusedForUnverifiedAccount(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()

	// Registered but never verified.
	if err := svc.Register(ctx, "unverified@example.com", goodPassword); err != nil {
		t.Fatal(err)
	}
	m.last = mailMessageZero()

	if err := svc.RequestMagicLink(ctx, "unverified@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	if m.last.Subject != "" {
		t.Fatalf("an unverified account must not get a sign-in link, got %q", m.last.Subject)
	}
}

// A magic link is a FIRST factor: it proves control of the inbox and nothing
// more. An account with two-step verification must still clear it, or adding
// this route would have quietly become a way around 2FA.
func TestMagicLinkStillRequiresTheSecondFactor(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	uid, secret, _ := enrolTOTP(t, svc, m, "magic2fa@example.com")
	_ = uid

	if err := svc.RequestMagicLink(ctx, "magic2fa@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	challenge, code := linkParts(t, m.last.Text)

	r, err := svc.ConfirmMagicLink(ctx, challenge, code, webInfo())
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("an account with an authenticator must still be challenged")
	}
	if r.SessionToken != "" {
		t.Fatal("a challenge must never come with a session token")
	}

	// And the challenge completes normally.
	totpCode, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, totpCode, webInfo()); err != nil {
		t.Fatalf("finishing the challenge should sign in: %v", err)
	}
}

func TestMagicLinkWrongCodeIsCappedAndThenDies(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "magiccap@example.com")

	if err := svc.RequestMagicLink(ctx, "magiccap@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	challenge, code := linkParts(t, m.last.Text)

	for i := range 4 {
		if _, err := svc.ConfirmMagicLink(ctx, challenge, "000000", webInfo()); err != auth.ErrInvalidCredentials {
			t.Fatalf("attempt %d: want ErrInvalidCredentials, got %v", i+1, err)
		}
	}
	if _, err := svc.ConfirmMagicLink(ctx, challenge, "000000", webInfo()); err != auth.ErrTooManyAttempts {
		t.Fatalf("want ErrTooManyAttempts, got %v", err)
	}
	// Destroyed, so even the right code cannot revive it.
	if _, err := svc.ConfirmMagicLink(ctx, challenge, code, webInfo()); err != auth.ErrChallengeExpired {
		t.Fatalf("a spent link must be gone, got %v", err)
	}
}

// The code in the email is drawn independently of the link token. If it were a
// truncation of it, the shorter value would weaken the longer one rather than
// being an alternative to it.
func TestMagicCodeIsNotDerivedFromTheLinkToken(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "magicindep@example.com")

	if err := svc.RequestMagicLink(ctx, "magicindep@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	challenge, code := linkParts(t, m.last.Text)
	if strings.Contains(challenge, code) {
		t.Fatalf("the code %q appears inside the link token %q", code, challenge)
	}
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 digits", code)
	}
}

// Looking a link up must not consume it: the interstitial checks whether it is
// still live before showing the user a button.
func TestLookupDoesNotConsumeTheLink(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "magiclook@example.com")

	if err := svc.RequestMagicLink(ctx, "magiclook@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	challenge, code := linkParts(t, m.last.Text)

	for range 3 {
		if err := svc.LookupMagicLink(ctx, challenge); err != nil {
			t.Fatalf("looking up a live link should succeed: %v", err)
		}
	}
	if _, err := svc.ConfirmMagicLink(ctx, challenge, code, webInfo()); err != nil {
		t.Fatalf("the link should still work after being looked at: %v", err)
	}
}

func TestMagicLinkForDisabledAccountSendsNothing(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "magicdisabled@example.com")

	u, err := st.GetUserByEmail(ctx, "magicdisabled@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserDisabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	m.last = mailMessageZero()

	if err := svc.RequestMagicLink(ctx, "magicdisabled@example.com", webInfo()); err != nil {
		t.Fatal(err)
	}
	if m.last.Subject != "" {
		t.Fatalf("a suspended account must not get a sign-in link, got %q", m.last.Subject)
	}
}
