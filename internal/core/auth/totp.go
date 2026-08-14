package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/store"
)

const (
	// totpPeriod is the step length every authenticator app assumes.
	totpPeriod = 30
	// totpSkew accepts one step either side of now, covering ordinary clock
	// drift on a phone. Wider would multiply the codes valid at any moment.
	totpSkew = 1
	// recoveryCodeCount is how many single-use codes are issued at enrolment.
	recoveryCodeCount = 10
)

// ErrTOTPNotEnrolled means there is no confirmed authenticator for this user.
var ErrTOTPNotEnrolled = errors.New("no authenticator app is set up")

// ErrLastFactor refuses an action that would leave an account with a second
// factor recorded but no way to satisfy it.
var ErrLastFactor = errors.New("that is the only sign-in method left")

// TOTPEnrolment is what the setup screen needs to show.
type TOTPEnrolment struct {
	// Secret in base32, shown so the user can type it into an app that cannot
	// scan a QR code.
	Secret string
	// URI is the otpauth:// value encoded in the QR image.
	URI string
}

// BeginTOTPEnrolment generates a new secret and stores it unconfirmed.
//
// The secret is sealed with the shared AES key before it touches the database.
// It stays unconfirmed until a code proves the user actually scanned it —
// activating on generation would let someone lock themselves out by closing the
// setup page.
func (s *Service) BeginTOTPEnrolment(ctx context.Context, userID int64) (TOTPEnrolment, error) {
	if s.sealer == nil {
		return TOTPEnrolment{}, ErrSealerUnavailable
	}
	// Refuse when an authenticator is already confirmed. Starting enrolment
	// replaces the stored secret and clears confirmed_at, so allowing it here
	// would let a stale page or a direct POST silently strip a working second
	// factor — the user would not find out until their next sign-in. Turning it
	// off is an explicit, separately-confirmed action.
	if t, err := s.store.GetTOTP(ctx, userID); err == nil && t.Confirmed() {
		return TOTPEnrolment{}, ErrAlreadyEnrolled
	} else if err != nil && !errors.Is(err, store.ErrNoTOTP) {
		return TOTPEnrolment{}, err
	}
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return TOTPEnrolment{}, err
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.brand,
		AccountName: u.Email,
		Period:      totpPeriod,
		SecretSize:  20, // 160 bits, the RFC 4226 recommendation
	})
	if err != nil {
		return TOTPEnrolment{}, fmt.Errorf("generate totp: %w", err)
	}
	sealed, err := s.sealer.Seal(key.Secret())
	if err != nil {
		return TOTPEnrolment{}, fmt.Errorf("seal totp secret: %w", err)
	}
	if err := s.store.StartTOTPEnrolment(ctx, userID, sealed); err != nil {
		return TOTPEnrolment{}, err
	}
	return TOTPEnrolment{Secret: key.Secret(), URI: key.URL()}, nil
}

// TOTPQRCode renders the pending enrolment's QR image as PNG bytes.
//
// Regenerated from the stored secret on demand rather than cached: the image is
// a credential, and holding it anywhere other than the one row is another place
// it could leak from.
func (s *Service) TOTPQRCode(ctx context.Context, userID int64) ([]byte, error) {
	secret, _, err := s.totpSecret(ctx, userID)
	if err != nil {
		return nil, err
	}
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	key, err := otp.NewKeyFromURL(otpauthURL(s.brand, u.Email, secret))
	if err != nil {
		return nil, fmt.Errorf("build totp key: %w", err)
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return nil, fmt.Errorf("totp qr: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode qr: %w", err)
	}
	return buf.Bytes(), nil
}

// TwoFactorAvailable reports whether authenticator enrolment can work at all,
// which requires a configured encryption key.
func (s *Service) TwoFactorAvailable() bool { return s.sealer != nil }

// PendingTOTPEnrolment returns the in-progress enrolment so a failed confirm
// can re-render the same QR code rather than making the user scan a new one.
// Reports ErrTOTPNotEnrolled when there is nothing pending.
func (s *Service) PendingTOTPEnrolment(ctx context.Context, userID int64) (TOTPEnrolment, error) {
	secret, t, err := s.totpSecret(ctx, userID)
	if err != nil {
		return TOTPEnrolment{}, err
	}
	if t.Confirmed() {
		return TOTPEnrolment{}, ErrAlreadyEnrolled
	}
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return TOTPEnrolment{}, err
	}
	return TOTPEnrolment{Secret: secret, URI: otpauthURL(s.brand, u.Email, secret)}, nil
}

// ConfirmTOTP completes enrolment and returns freshly minted recovery codes.
//
// The codes are returned once and never again — only their hashes are stored,
// so there is nothing left to show later. That is deliberate: it means a
// database read cannot hand an attacker a way past the second factor.
func (s *Service) ConfirmTOTP(ctx context.Context, userID int64, code string) ([]string, error) {
	secret, t, err := s.totpSecret(ctx, userID)
	if err != nil {
		return nil, err
	}
	if t.Confirmed() {
		return nil, ErrAlreadyEnrolled
	}
	step, ok := validateTOTP(normalizeCode(code), secret, time.Now())
	if !ok {
		return nil, ErrInvalidCredentials
	}
	if err := s.store.ConfirmTOTPStep(ctx, userID, step); err != nil {
		return nil, err
	}
	codes, err := s.issueRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventTOTPEnabled})
	return codes, nil
}

// verifyTOTPCode checks a code during sign-in and claims its step.
//
// The claim is what stops replay: a code phished and reused within its own
// window fails because the step is no longer newer than the stored one.
func (s *Service) verifyTOTPCode(ctx context.Context, userID int64, code string) (bool, error) {
	secret, t, err := s.totpSecret(ctx, userID)
	if errors.Is(err, ErrTOTPNotEnrolled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !t.Confirmed() {
		return false, nil
	}
	step, ok := validateTOTP(normalizeCode(code), secret, time.Now())
	if !ok {
		return false, nil
	}
	return s.store.UseTOTPStep(ctx, userID, step)
}

// FactorChange reports what a security toggle actually did, so the surface can
// tell the user rather than leaving them to infer it.
//
// Both fields matter for the same reason: recovery codes appear and disappear
// as a side effect of turning factors on and off, and a change the user is not
// shown is one they will discover at the worst possible moment — holding a
// printout that no longer works, or believing they hold codes they were never
// given.
type FactorChange struct {
	// RecoveryCodesCleared is true when turning the factor off also invalidated
	// the user's recovery codes, because nothing was left to recover past.
	RecoveryCodesCleared bool
	// NewRecoveryCodes are codes minted by this change. Returned exactly once —
	// only their hashes are stored, so an unshown code is a code nobody has.
	NewRecoveryCodes []string
}

// DisableTOTP removes the authenticator enrolment.
func (s *Service) DisableTOTP(ctx context.Context, userID int64) (FactorChange, error) {
	var change FactorChange
	if err := s.store.DeleteTOTP(ctx, userID); err != nil {
		return change, err
	}
	// Recovery codes exist to get past a second factor. With none left they are
	// just unused credentials sitting in the database, so they go too — but the
	// user has to be told, because they may be holding a printout of them.
	remaining, err := s.enabledFactors(ctx, userID)
	if err == nil && len(remaining) == 0 {
		if n, err := s.store.CountUnusedRecoveryCodes(ctx, userID); err == nil && n > 0 {
			change.RecoveryCodesCleared = true
		}
		_ = s.store.DeleteRecoveryCodes(ctx, userID)
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{
		Kind:         emailtpl.SecurityEventTOTPDisabled,
		CodesCleared: change.RecoveryCodesCleared,
	})
	return change, nil
}

// SetEmailOTPEnabled turns emailed sign-in codes on or off.
func (s *Service) SetEmailOTPEnabled(ctx context.Context, userID int64, on bool) (FactorChange, error) {
	var change FactorChange
	if err := s.store.SetEmailOTPEnabled(ctx, userID, on); err != nil {
		return change, err
	}
	if !on {
		if remaining, err := s.enabledFactors(ctx, userID); err == nil && len(remaining) == 0 {
			if n, err := s.store.CountUnusedRecoveryCodes(ctx, userID); err == nil && n > 0 {
				change.RecoveryCodesCleared = true
			}
			_ = s.store.DeleteRecoveryCodes(ctx, userID)
		}
	} else if n, err := s.store.CountUnusedRecoveryCodes(ctx, userID); err == nil && n == 0 {
		// Turning on a factor with no way past it is how people get locked out
		// of their own money, so codes are issued alongside it rather than
		// offered as an optional extra step. They are handed back so the caller
		// can show them: only hashes are stored, so a code never displayed is a
		// code the user does not have.
		codes, err := s.issueRecoveryCodes(ctx, userID)
		if err != nil {
			return change, err
		}
		change.NewRecoveryCodes = codes
	}
	kind := emailtpl.SecurityEventEmailOTPDisabled
	if on {
		kind = emailtpl.SecurityEventEmailOTPEnabled
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{
		Kind:         kind,
		CodesCleared: change.RecoveryCodesCleared,
	})
	return change, nil
}

// RegenerateRecoveryCodes issues a fresh set, invalidating every previous code,
// and tells the account holder. Use it for a user-initiated regeneration.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	codes, err := s.issueRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventRecoveryRegenerated})
	return codes, nil
}

// issueRecoveryCodes replaces the set without notifying.
//
// Separate from the public method so that turning a factor on, which mints
// codes as a side effect, sends one email describing that change rather than
// two describing halves of it.
func (s *Service) issueRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		c, err := recoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, c)
		hashes = append(hashes, HashToken(normalizeCode(c)))
	}
	if err := s.store.ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// SecurityState drives the Security tab.
type SecurityState struct {
	TOTPEnabled       bool
	TOTPPending       bool
	EmailOTPEnabled   bool
	RecoveryRemaining int
	HasPassword       bool
}

func (s *Service) SecurityOverview(ctx context.Context, userID int64) (SecurityState, error) {
	var st SecurityState
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return st, err
	}
	st.HasPassword = u.PasswordHash != ""
	st.EmailOTPEnabled = false
	if on, err := s.store.EmailOTPEnabled(ctx, userID); err == nil {
		st.EmailOTPEnabled = on
	}
	switch t, err := s.store.GetTOTP(ctx, userID); {
	case err == nil && t.Confirmed():
		st.TOTPEnabled = true
	case err == nil:
		st.TOTPPending = true
	case !errors.Is(err, store.ErrNoTOTP):
		return st, err
	}
	if n, err := s.store.CountUnusedRecoveryCodes(ctx, userID); err == nil {
		st.RecoveryRemaining = n
	}
	return st, nil
}

// notifySecurityChange emails the account holder that their sign-in settings
// moved. Best-effort: the change has already happened, and failing the request
// now would misreport what actually took place.
func (s *Service) notifySecurityChange(ctx context.Context, userID int64, ev emailtpl.SecurityEvent) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return
	}
	msg, err := emailtpl.SecurityChanged(s.localeFor(ctx, userID), ev)
	if err != nil {
		return
	}
	msg.To = u.Email
	_ = s.mailer.Send(ctx, msg)
}

// totpSecret unseals the stored secret.
func (s *Service) totpSecret(ctx context.Context, userID int64) (string, store.TOTP, error) {
	if s.sealer == nil {
		return "", store.TOTP{}, ErrSealerUnavailable
	}
	t, err := s.store.GetTOTP(ctx, userID)
	if errors.Is(err, store.ErrNoTOTP) {
		return "", store.TOTP{}, ErrTOTPNotEnrolled
	}
	if err != nil {
		return "", store.TOTP{}, err
	}
	secret, err := s.sealer.Open(t.Secret)
	if err != nil {
		// Almost always a rotated or lost encryption key. Say so plainly: the
		// alternative is a bare "invalid code" that sends the user hunting for
		// a problem with their phone.
		return "", store.TOTP{}, fmt.Errorf("%w: %w", ErrSealedSecretUnreadable, err)
	}
	return secret, t, nil
}

// validateTOTP checks a code against the secret across the accepted skew and
// returns the step that matched.
//
// pquerna/otp validates but does not report which step matched, and the step is
// exactly what the replay guard needs — so the window is walked here.
func validateTOTP(code, secret string, now time.Time) (int64, bool) {
	for delta := -totpSkew; delta <= totpSkew; delta++ {
		at := now.Add(time.Duration(delta) * totpPeriod * time.Second)
		ok, err := totp.ValidateCustom(code, secret, at, totp.ValidateOpts{
			Period: totpPeriod,
			Skew:   0, // stepping by hand; let this check exactly one step
			Digits: otp.DigitsSix,
		})
		if err == nil && ok {
			return at.Unix() / totpPeriod, true
		}
	}
	return 0, false
}

// otpauthURL rebuilds the provisioning URI from a stored secret.
func otpauthURL(issuer, account, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=%d",
		urlEscape(issuer), urlEscape(account), secret, urlEscape(issuer), totpPeriod)
}

// recoveryCode returns a code in the form "abcd-efgh-ijkl": base32 without
// padding, so it is unambiguous read aloud and safe to type in any case.
func recoveryCode() (string, error) {
	b := make([]byte, 8) // 64 bits
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	raw := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	return raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12], nil
}

func urlEscape(s string) string {
	return strings.NewReplacer(" ", "%20", ":", "%3A", "/", "%2F", "?", "%3F", "&", "%26", "=", "%3D").Replace(s)
}
