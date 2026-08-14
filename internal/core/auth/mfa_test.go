package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/store"
)

// testKey is a fixed 32-byte hex key; the tests need sealing to work, not to be
// secret.
const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// newMFAService builds a service with a sealer, so TOTP enrolment is available.
func newMFAService(t *testing.T) (*auth.Service, *capMailer, *store.Store) {
	t.Helper()
	sealer, err := crypto.NewSealer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(openTestDB(t))
	m := &capMailer{}
	svc := auth.NewService(st, m, "http://localhost:8080", auth.Config{Sealer: sealer})
	return svc, m, st
}

// enrolTOTP registers a verified user with a confirmed authenticator and
// returns their id, the base32 secret, and the recovery codes.
func enrolTOTP(t *testing.T, svc *auth.Service, m *capMailer, email string) (int64, string, []string) {
	t.Helper()
	ctx := context.Background()
	registerVerified(t, svc, m, email)

	tok, err := svc.Login(ctx, email, goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}

	enr, err := svc.BeginTOTPEnrolment(ctx, u.ID)
	if err != nil {
		t.Fatalf("begin enrolment: %v", err)
	}
	code, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := svc.ConfirmTOTP(ctx, u.ID, code)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	return u.ID, enr.Secret, codes
}

// nextStepCode returns a code from the step AFTER now.
//
// Confirming an enrolment consumes the step that proved it, so a sign-in code
// drawn from that same 30-second window is correctly rejected as a replay.
// Real users hit this only if they enrol and immediately sign out and back in
// within one window; tests hit it every time, so they step forward.
func nextStepCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestEnrolmentIsNotActiveUntilConfirmed(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "pending@example.com")

	tok, err := svc.Login(ctx, "pending@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginTOTPEnrolment(ctx, u.ID); err != nil {
		t.Fatal(err)
	}

	// Abandoning setup must not lock the user out of their own account.
	r, err := svc.BeginLogin(ctx, "pending@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if r.NeedsChallenge() {
		t.Fatal("an unconfirmed enrolment must not become an active factor")
	}
}

func TestTOTPLoginRequiresChallenge(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	_, secret, _ := enrolTOTP(t, svc, m, "totp@example.com")

	r, err := svc.BeginLogin(ctx, "totp@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("an enrolled account must get a challenge, not a session")
	}
	if r.SessionToken != "" {
		t.Fatal("a challenge must never come with a session token")
	}
	if !strings.Contains(r.MaskedEmail, "@") || strings.Contains(r.MaskedEmail, "totp@") {
		t.Fatalf("masked email leaks the address: %q", r.MaskedEmail)
	}

	session, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, nextStepCode(t, secret), webInfo())
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := svc.AuthenticateSession(ctx, session); err != nil {
		t.Fatal("the issued session should authenticate")
	}
}

// Without a replay guard a phished code stays usable for the rest of its
// 30-second window, which is exactly long enough to matter.
func TestTOTPCodeCannotBeReplayed(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	_, secret, _ := enrolTOTP(t, svc, m, "replay@example.com")

	code := nextStepCode(t, secret)

	first, err := svc.BeginLogin(ctx, "replay@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, first.ChallengeToken, auth.FactorTOTP, code, webInfo()); err != nil {
		t.Fatalf("first use should succeed: %v", err)
	}

	second, err := svc.BeginLogin(ctx, "replay@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, second.ChallengeToken, auth.FactorTOTP, code, webInfo()); err == nil {
		t.Fatal("the same code must not be accepted twice")
	}
}

// A phone's clock drifts, so one step either side is accepted. Forward skew is
// checked at sign-in; backward skew is checked at enrolment, because after a
// step has been used a code from an earlier one is rejected by the replay guard
// rather than by the skew window — testing it through sign-in would prove the
// wrong thing.
func TestTOTPAcceptsOneStepOfSkew(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	_, secret, _ := enrolTOTP(t, svc, m, "skew@example.com")

	r, err := svc.BeginLogin(ctx, "skew@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	// One step ahead of now.
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, nextStepCode(t, secret), webInfo()); err != nil {
		t.Fatalf("a code one step ahead should be accepted: %v", err)
	}

	// Far outside the window.
	stale, err := totp.GenerateCode(secret, time.Now().Add(-5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.BeginLogin(ctx, "skew@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r2.ChallengeToken, auth.FactorTOTP, stale, webInfo()); err == nil {
		t.Fatal("a code far outside the window must be rejected")
	}
}

// Backward skew: a code from the previous step still completes enrolment, which
// is where a slow phone clock actually shows up.
func TestTOTPConfirmAcceptsPreviousStep(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "backskew@example.com")
	tok, err := svc.Login(ctx, "backskew@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	enr, err := svc.BeginTOTPEnrolment(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := totp.GenerateCode(enr.Secret, time.Now().Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmTOTP(ctx, u.ID, previous); err != nil {
		t.Fatalf("a code one step behind should confirm enrolment: %v", err)
	}
}

func TestChallengeDiesAfterTooManyAttempts(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	_, secret, _ := enrolTOTP(t, svc, m, "attempts@example.com")

	r, err := svc.BeginLogin(ctx, "attempts@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, "000000", webInfo()); err != auth.ErrInvalidCredentials {
			t.Fatalf("attempt %d: want ErrInvalidCredentials, got %v", i+1, err)
		}
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, "000000", webInfo()); err != auth.ErrTooManyAttempts {
		t.Fatalf("want ErrTooManyAttempts, got %v", err)
	}

	// The challenge is destroyed, so even the RIGHT code cannot revive it.
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, nextStepCode(t, secret), webInfo()); err != auth.ErrChallengeExpired {
		t.Fatalf("a spent challenge must be gone, got %v", err)
	}
}

func TestRecoveryCodeIsSingleUse(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	_, _, codes := enrolTOTP(t, svc, m, "recovery@example.com")
	if len(codes) != 10 {
		t.Fatalf("recovery codes = %d, want 10", len(codes))
	}

	r, err := svc.BeginLogin(ctx, "recovery@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorRecovery, codes[0], webInfo()); err != nil {
		t.Fatalf("recovery code should work: %v", err)
	}

	r2, err := svc.BeginLogin(ctx, "recovery@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r2.ChallengeToken, auth.FactorRecovery, codes[0], webInfo()); err == nil {
		t.Fatal("a recovery code must not be usable twice")
	}
	// A different code from the set still works.
	if _, err := svc.CompleteChallenge(ctx, r2.ChallengeToken, auth.FactorRecovery, codes[1], webInfo()); err != nil {
		t.Fatalf("an unused recovery code should still work: %v", err)
	}
}

func TestRegeneratingRecoveryCodesInvalidatesOldOnes(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	uid, _, old := enrolTOTP(t, svc, m, "regen@example.com")

	fresh, err := svc.RegenerateRecoveryCodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.BeginLogin(ctx, "regen@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorRecovery, old[0], webInfo()); err == nil {
		t.Fatal("regenerating must invalidate every previous code")
	}
	r2, err := svc.BeginLogin(ctx, "regen@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r2.ChallengeToken, auth.FactorRecovery, fresh[0], webInfo()); err != nil {
		t.Fatalf("a fresh code should work: %v", err)
	}
}

func TestEmailOTPFlow(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "otp@example.com")

	tok, err := svc.Login(ctx, "otp@example.com", goodPassword, webInfo())
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

	r, err := svc.BeginLogin(ctx, "otp@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("email OTP should require a challenge")
	}
	// The code is sent without the user asking, since there is nothing to decide.
	code := digitsFrom(t, m.last.Text)
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorEmailOTP, code, webInfo()); err != nil {
		t.Fatalf("email code should work: %v", err)
	}
}

// The emailed code must never be reusable after the challenge is consumed.
func TestEmailCodeCannotBeReusedOnANewChallenge(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "reuse@example.com")
	tok, _ := svc.Login(ctx, "reuse@example.com", goodPassword, webInfo())
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEmailOTPEnabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}

	first, err := svc.BeginLogin(ctx, "reuse@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	code := digitsFrom(t, m.last.Text)
	if _, err := svc.CompleteChallenge(ctx, first.ChallengeToken, auth.FactorEmailOTP, code, webInfo()); err != nil {
		t.Fatal(err)
	}

	second, err := svc.BeginLogin(ctx, "reuse@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, second.ChallengeToken, auth.FactorEmailOTP, code, webInfo()); err == nil {
		t.Fatal("an old code must not satisfy a new challenge")
	}
}

// The deprecated shim must fail closed: it cannot express a challenge, so it
// must refuse rather than hand back a session.
func TestLoginShimRefusesMFAAccounts(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	enrolTOTP(t, svc, m, "shim@example.com")

	tok, err := svc.Login(ctx, "shim@example.com", goodPassword, webInfo())
	if err != auth.ErrMFARequired {
		t.Fatalf("want ErrMFARequired, got %v", err)
	}
	if tok != "" {
		t.Fatal("the shim must never return a session for an MFA account")
	}
}

func TestDisablingTOTPClearsRecoveryCodes(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	uid, _, _ := enrolTOTP(t, svc, m, "disable@example.com")

	if _, err := svc.DisableTOTP(ctx, uid); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountUnusedRecoveryCodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovery codes = %d, want 0 once there is nothing to recover past", n)
	}

	r, err := svc.BeginLogin(ctx, "disable@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if r.NeedsChallenge() {
		t.Fatal("no factors left, so no challenge")
	}
}

func TestSecurityOverview(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	uid, _, _ := enrolTOTP(t, svc, m, "overview@example.com")

	st, err := svc.SecurityOverview(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !st.TOTPEnabled || st.TOTPPending {
		t.Fatalf("want confirmed TOTP, got %+v", st)
	}
	if st.RecoveryRemaining != 10 {
		t.Fatalf("recovery remaining = %d, want 10", st.RecoveryRemaining)
	}
	if !st.HasPassword {
		t.Fatal("this account has a password")
	}
}

// Enrolment must refuse rather than store a secret in the clear.
func TestEnrolmentRefusedWithoutEncryptionKey(t *testing.T) {
	st := store.New(openTestDB(t))
	svc := auth.NewService(st, &capMailer{}, "http://localhost:8080", auth.Config{})
	ctx := context.Background()

	uid, err := st.CreateUser(ctx, "nokey@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginTOTPEnrolment(ctx, uid); err != auth.ErrSealerUnavailable {
		t.Fatalf("want ErrSealerUnavailable, got %v", err)
	}
}

func TestMaskEmail(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"steven@example.com", "s••••n@example.com"},
		{"ab@example.com", "••@example.com"},
		{"a@example.com", "•@example.com"},
		{"not-an-email", "not-an-email"},
	} {
		if got := auth.MaskEmail(tc.in); got != tc.want {
			t.Errorf("MaskEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNumericCodeIsAlwaysFullLength(t *testing.T) {
	// A code rendered without its leading zeros would leak that the value was
	// small, and would not match what the user was shown.
	for range 200 {
		c, err := auth.NumericCode(6)
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != 6 {
			t.Fatalf("code %q is not 6 digits", c)
		}
		if strings.ContainsFunc(c, func(r rune) bool { return r < '0' || r > '9' }) {
			t.Fatalf("code %q is not all digits", c)
		}
	}
}

// digitsFrom pulls the 6-digit code out of an email body.
func digitsFrom(t *testing.T, body string) string {
	t.Helper()
	run := 0
	start := -1
	for i, r := range body {
		if r >= '0' && r <= '9' {
			if run == 0 {
				start = i
			}
			run++
			if run == 6 {
				return body[start : start+6]
			}
		} else {
			run = 0
		}
	}
	t.Fatalf("no 6-digit code in body: %q", body)
	return ""
}

// Starting enrolment replaces the secret and clears confirmed_at, so it must
// refuse when an authenticator is already active — otherwise a stale page or a
// direct POST silently strips a working second factor, and the user only finds
// out at their next sign-in.
func TestBeginEnrolmentRefusesWhenAlreadyEnrolled(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	uid, secret, _ := enrolTOTP(t, svc, m, "already@example.com")

	if _, err := svc.BeginTOTPEnrolment(ctx, uid); err != auth.ErrAlreadyEnrolled {
		t.Fatalf("want ErrAlreadyEnrolled, got %v", err)
	}
	// The original authenticator must still work.
	r, err := svc.BeginLogin(ctx, "already@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("the existing enrolment should still require a challenge")
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorTOTP, nextStepCode(t, secret), webInfo()); err != nil {
		t.Fatalf("the original secret must still verify: %v", err)
	}
}

// Turning off the authenticator while email codes are still on must KEEP the
// recovery codes: there is still a second factor to get past, and silently
// destroying the way past it is how someone ends up locked out of their own
// money when they later lose access to their inbox.
func TestDisablingTOTPKeepsRecoveryCodesWhenEmailOTPRemains(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	uid, _, codes := enrolTOTP(t, svc, m, "keepcodes@example.com")

	if _, err := svc.SetEmailOTPEnabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DisableTOTP(ctx, uid); err != nil {
		t.Fatal(err)
	}

	n, err := st.CountUnusedRecoveryCodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(codes) {
		t.Fatalf("recovery codes = %d, want %d kept while email codes are still on", n, len(codes))
	}

	// And they must still actually work as a factor.
	r, err := svc.BeginLogin(ctx, "keepcodes@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if !r.NeedsChallenge() {
		t.Fatal("email codes are still on, so a challenge is expected")
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorRecovery, codes[0], webInfo()); err != nil {
		t.Fatalf("a kept recovery code should still sign the user in: %v", err)
	}
}

// Turning off the LAST factor drops the codes, whichever factor was last off.
func TestDisablingEmailOTPLastAlsoClearsRecoveryCodes(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	uid, _, _ := enrolTOTP(t, svc, m, "lastoff@example.com")

	if _, err := svc.SetEmailOTPEnabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DisableTOTP(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEmailOTPEnabled(ctx, uid, false); err != nil {
		t.Fatal(err)
	}

	n, err := st.CountUnusedRecoveryCodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovery codes = %d, want 0 once no factor remains", n)
	}
}

// Re-enabling a factor after everything was turned off must issue a fresh set,
// not leave the account with a second factor and no way past it.
func TestReEnablingAFactorIssuesFreshRecoveryCodes(t *testing.T) {
	svc, m, st := newMFAService(t)
	ctx := context.Background()
	uid, _, old := enrolTOTP(t, svc, m, "reenable@example.com")

	if _, err := svc.DisableTOTP(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEmailOTPEnabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}

	n, err := st.CountUnusedRecoveryCodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if n != recoveryCodeCountForTest {
		t.Fatalf("recovery codes = %d, want a fresh set", n)
	}

	// The pre-disable codes must not come back.
	r, err := svc.BeginLogin(ctx, "reenable@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteChallenge(ctx, r.ChallengeToken, auth.FactorRecovery, old[0], webInfo()); err == nil {
		t.Fatal("codes from before the factor was disabled must not still work")
	}
}

// recoveryCodeCountForTest mirrors the service's issue count.
const recoveryCodeCountForTest = 10

// Enabling a factor issues recovery codes so the user is never left with a
// second step and no way past it — but codes the user is never SHOWN are worse
// than none: the security page reports ten available while the user holds zero,
// and only the hashes exist, so they can never be recovered or displayed later.
func TestEnablingEmailOTPReturnsTheCodesItIssues(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "issued@example.com")
	tok, err := svc.Login(ctx, "issued@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}

	change, err := svc.SetEmailOTPEnabled(ctx, u.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.NewRecoveryCodes) != recoveryCodeCountForTest {
		t.Fatalf("issued codes returned = %d, want %d — the user must be shown what was created",
			len(change.NewRecoveryCodes), recoveryCodeCountForTest)
	}
}

// Each security change sends exactly one email that names what happened. One
// email per change, not two describing halves of it: turning a factor on mints
// recovery codes as a side effect, and a second "new codes generated" notice
// would read like a separate event the user did not cause.
func TestSecurityChangeEmailsNameWhatHappened(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	uid, _, _ := enrolTOTP(t, svc, m, "notify@example.com")

	// Enrolment already sent one; check it named the change.
	if !strings.Contains(m.last.Text, "authenticator app was turned ON") {
		t.Errorf("enrolment email should name the change, got: %q", m.last.Text)
	}

	// Turning on email codes while TOTP is on: codes already exist, so nothing
	// is minted and nothing is cleared.
	if _, err := svc.SetEmailOTPEnabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.last.Text, "email code was turned ON") {
		t.Errorf("expected the email-code change to be named, got: %q", m.last.Text)
	}
	if strings.Contains(m.last.Text, "recovery codes were deleted") {
		t.Error("nothing was deleted here")
	}

	// Disabling TOTP while email codes remain: codes survive, so the email must
	// not claim otherwise.
	if _, err := svc.DisableTOTP(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.last.Text, "authenticator app was turned OFF") {
		t.Errorf("expected the disable to be named, got: %q", m.last.Text)
	}
	if strings.Contains(m.last.Text, "recovery codes were deleted") {
		t.Error("email codes are still on, so the recovery codes were kept")
	}

	// Turning off the last factor DOES clear them, and the email says so.
	if _, err := svc.SetEmailOTPEnabled(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.last.Text, "recovery codes were deleted") {
		t.Errorf("the last factor going off deletes the codes; the email must say so, got: %q", m.last.Text)
	}
	if !strings.Contains(m.last.Text, "no longer work") {
		t.Errorf("a saved printout is now useless and the email must say it, got: %q", m.last.Text)
	}
}

// A user-initiated regeneration is its own event and is notified; the same
// operation performed as a side effect of enabling a factor is not, or the user
// would get two emails for one action.
func TestRegenerateNotifiesButSideEffectDoesNot(t *testing.T) {
	svc, m, _ := newMFAService(t)
	ctx := context.Background()
	registerVerified(t, svc, m, "regen-notify@example.com")
	tok, err := svc.Login(ctx, "regen-notify@example.com", goodPassword, webInfo())
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.AuthenticateSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}

	// Side effect: enabling a factor mints codes but reports the FACTOR change.
	if _, err := svc.SetEmailOTPEnabled(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.last.Text, "New recovery codes were generated") {
		t.Errorf("enabling a factor should report the factor, not a separate code event: %q", m.last.Text)
	}

	// Explicit regeneration is its own event.
	if _, err := svc.RegenerateRecoveryCodes(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.last.Text, "New recovery codes were generated") {
		t.Errorf("an explicit regeneration should be notified, got: %q", m.last.Text)
	}
}
