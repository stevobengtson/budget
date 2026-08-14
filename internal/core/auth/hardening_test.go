package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/store"
)

const goodPassword = "correct-horse-battery"

func webInfo() store.SessionInfo {
	return store.SessionInfo{UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X) Chrome/120 Safari/537", IP: "127.0.0.1"}
}

// registerVerified creates a usable, email-verified account.
func registerVerified(t *testing.T, svc *auth.Service, m *capMailer, email string) {
	t.Helper()
	ctx := context.Background()
	if err := svc.Register(ctx, email, goodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.VerifyEmail(ctx, linkToken(t, m.last.Text, "token=")); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// The policy has to live in the service, not the handlers: the JSON API and
// later phases create passwords through paths no handler ever sees.
func TestPasswordPolicyEnforcedInService(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if err := svc.Register(ctx, "short@example.com", "abc"); err != auth.ErrPasswordTooShort {
		t.Fatalf("want ErrPasswordTooShort, got %v", err)
	}
	// Unbounded input into argon2id is a CPU exhaustion vector, not a style nit.
	if err := svc.Register(ctx, "long@example.com", strings.Repeat("x", auth.MaxPasswordLength+1)); err != auth.ErrPasswordTooLong {
		t.Fatalf("want ErrPasswordTooLong, got %v", err)
	}
}

func TestLockoutAfterRepeatedFailures(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "target@example.com")

	p := auth.DefaultLockoutPolicy
	// Every attempt up to and including the one that trips the lock still
	// reports bad credentials: the lock is applied after the check, so it is
	// the *next* attempt that is turned away.
	for i := 0; i <= p.Threshold; i++ {
		if _, err := svc.Login(ctx, "target@example.com", "wrong-password", webInfo()); err != auth.ErrInvalidCredentials {
			t.Fatalf("attempt %d: want ErrInvalidCredentials, got %v", i+1, err)
		}
	}

	_, err := svc.Login(ctx, "target@example.com", "wrong-password", webInfo())
	wait, locked := auth.LockedOut(err)
	if !locked {
		t.Fatalf("want a lockout after %d failures, got %v", p.Threshold+1, err)
	}
	if wait <= 0 || wait > p.Base {
		t.Fatalf("retry-after = %v, want (0, %v]", wait, p.Base)
	}

	// The lock must hold even against the CORRECT password — otherwise it does
	// nothing to slow an attacker who is about to guess right.
	if _, err := svc.Login(ctx, "target@example.com", goodPassword, webInfo()); err == nil {
		t.Fatal("a locked account must not accept the correct password either")
	}
}

// Counting failures only for real accounts would make "this address never locks"
// a reliable way to enumerate which emails are registered.
func TestUnknownAddressesLockToo(t *testing.T) {
	svc, _, st := newService(t)
	ctx := context.Background()

	p := auth.DefaultLockoutPolicy
	for i := 0; i <= p.Threshold; i++ {
		_, _ = svc.Login(ctx, "ghost@example.com", "whatever", webInfo())
	}

	l, err := st.GetLockout(ctx, store.ScopePasswordLogin, "ghost@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !l.Locked(time.Now()) {
		t.Fatal("an address that does not exist must lock exactly like one that does")
	}
}

func TestSuccessfulLoginClearsFailures(t *testing.T) {
	svc, m, st := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "clears@example.com")

	for range 3 {
		_, _ = svc.Login(ctx, "clears@example.com", "wrong-password", webInfo())
	}
	if _, err := svc.Login(ctx, "clears@example.com", goodPassword, webInfo()); err != nil {
		t.Fatalf("login: %v", err)
	}

	l, err := st.GetLockout(ctx, store.ScopePasswordLogin, "clears@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if l.Failures != 0 {
		t.Fatalf("failures = %d, want 0 after a successful sign-in", l.Failures)
	}
}

func TestLockoutSendsOneAlertPerLock(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "alert@example.com")

	p := auth.DefaultLockoutPolicy
	for i := 0; i <= p.Threshold; i++ {
		_, _ = svc.Login(ctx, "alert@example.com", "wrong-password", webInfo())
	}
	if !strings.Contains(m.last.Subject, "Unusual sign-in") {
		t.Fatalf("expected a lockout alert, last subject was %q", m.last.Subject)
	}
	m.last = mailMessageZero()

	// Further blocked attempts must not send more mail, or the lockout becomes
	// a way to flood the victim's inbox.
	for range 3 {
		_, _ = svc.Login(ctx, "alert@example.com", "wrong-password", webInfo())
	}
	if m.last.Subject != "" {
		t.Fatalf("blocked attempts should send no further mail, got %q", m.last.Subject)
	}
}

func TestChangePasswordRevokesOtherSessionsButKeepsCurrent(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "rotate@example.com")

	current, err := svc.Login(ctx, "rotate@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	phone, err := svc.Login(ctx, "rotate@example.com", goodPassword, store.SessionInfo{Client: "ios"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, current)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ChangePassword(ctx, u.ID, goodPassword, "a-brand-new-password", current); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.AuthenticateSession(ctx, current); err != nil {
		t.Fatal("the device making the change should stay signed in")
	}
	// The phone's bearer token is a row in the same sessions table, so rotating
	// the password has to evict it. This is the whole point of the rotation.
	if _, err := svc.AuthenticateSession(ctx, phone); err == nil {
		t.Fatal("other devices must be signed out by a password change")
	}
}

func TestResetPasswordRevokesEverySession(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "reset@example.com")

	sess, err := svc.Login(ctx, "reset@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPasswordReset(ctx, "reset@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, linkToken(t, m.last.Text, "token="), "another-good-password"); err != nil {
		t.Fatal(err)
	}

	// A reset is the recovery path after a suspected compromise: nothing is kept.
	if _, err := svc.AuthenticateSession(ctx, sess); err == nil {
		t.Fatal("a password reset must revoke every existing session")
	}
}

func TestChangePasswordRejectsUnchangedPassword(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "same@example.com")
	tok, err := svc.Login(ctx, "same@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, u.ID, goodPassword, goodPassword, tok); err != auth.ErrSamePassword {
		t.Fatalf("want ErrSamePassword, got %v", err)
	}
}

func TestSessionListFlagsCurrentAndRevokeWorks(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "sessions@example.com")

	current, err := svc.Login(ctx, "sessions@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Login(ctx, "sessions@example.com", goodPassword, store.SessionInfo{Client: "android"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, current)
	if err != nil {
		t.Fatal(err)
	}

	views, err := svc.ListSessions(ctx, u.ID, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("sessions = %d, want 2", len(views))
	}
	var currentCount int
	var otherID int64
	for _, v := range views {
		if v.Current {
			currentCount++
			// A user agent should have produced something a human recognises.
			if v.Label == "" {
				t.Fatal("the web session should carry a device label")
			}
		} else {
			otherID = v.ID
		}
	}
	if currentCount != 1 {
		t.Fatalf("exactly one session should be flagged current, got %d", currentCount)
	}

	if err := svc.RevokeSession(ctx, u.ID, otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateSession(ctx, other); err == nil {
		t.Fatal("revoked session should no longer authenticate")
	}
	if _, err := svc.AuthenticateSession(ctx, current); err != nil {
		t.Fatal("the current session must be untouched")
	}
}

func TestStepUpWindow(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "stepup@example.com")

	tok, err := svc.Login(ctx, "stepup@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	// A session created seconds ago is inside the window: signing in IS proving
	// a factor, so the user is not asked again immediately.
	if svc.StepUpRequired(ctx, tok) {
		t.Fatal("a freshly created session should not need step-up")
	}
	// An unknown token must fail closed.
	if !svc.StepUpRequired(ctx, "not-a-real-token") {
		t.Fatal("an unrecognised session must require step-up")
	}
	if !svc.StepUpRequired(ctx, "") {
		t.Fatal("an absent session must require step-up")
	}
}

func TestAvailableStepUpFactors(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "factors@example.com")
	tok, err := svc.Login(ctx, "factors@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	factors, err := svc.AvailableStepUpFactors(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(factors) != 1 || factors[0] != auth.FactorPassword {
		t.Fatalf("factors = %v, want [password]", factors)
	}
}

func TestDeviceLabel(t *testing.T) {
	for _, tc := range []struct{ ua, want string }{
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", "Chrome on macOS"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1", "Safari on iPhone"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Gecko/20100101 Firefox/121.0", "Firefox on Windows"},
		// Edge and Opera both embed "Chrome/", so ordering decides the answer.
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36 Edg/120.0", "Edge on Windows"},
		{"", ""},
		{"curl/8.4.0", ""},
	} {
		if got := auth.DeviceLabel(tc.ua); got != tc.want {
			t.Errorf("DeviceLabel(%.40q) = %q, want %q", tc.ua, got, tc.want)
		}
	}
}
