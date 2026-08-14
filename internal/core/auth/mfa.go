package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/store"
)

const (
	// challengeTTL bounds how long a half-finished sign-in stays open.
	challengeTTL = 10 * time.Minute
	// codeTTL is how long an emailed code is valid. Shorter than the challenge
	// so a resend genuinely refreshes something.
	codeTTL = 10 * time.Minute
	// maxChallengeAttempts is how many wrong codes are tolerated before the
	// challenge is destroyed and the user must start again. Six digits is only
	// a million possibilities, so the attempt cap — not the code length — is
	// what makes guessing hopeless.
	maxChallengeAttempts = 5
	// codeDigits is the length of an emailed one-time code.
	codeDigits = 6
)

var (
	// ErrMFARequired is returned by the deprecated Login shim when the account
	// has a second factor it cannot express.
	ErrMFARequired = errors.New("multi-factor authentication required")
	// ErrChallengeExpired means the challenge is unknown, used, or timed out.
	ErrChallengeExpired = errors.New("sign-in attempt expired; start again")
	// ErrTooManyAttempts means the attempt allowance is spent and the challenge
	// has been destroyed.
	ErrTooManyAttempts = errors.New("too many incorrect codes")
	// ErrNoSuchFactor means the requested factor is not enabled for this user.
	ErrNoSuchFactor = errors.New("that sign-in method is not available")
)

// Factor identifiers used on the wire (challenge method lists, API responses).
const (
	FactorTOTP     Factor = "totp"
	FactorEmailOTP Factor = "email_otp"
	FactorRecovery Factor = "recovery_code"
)

// LoginResult is a discriminated union: exactly one of SessionToken and
// ChallengeToken is ever set.
//
// A caller that ignores ChallengeToken signs nobody in, which is the safe way
// to be wrong — the alternative shape, returning a session plus a "but you
// still need MFA" flag, fails open when the flag is dropped.
type LoginResult struct {
	SessionToken   string
	ChallengeToken string
	Methods        []Factor
	UserID         int64
	// MaskedEmail is shown on the challenge screen so the user knows which
	// inbox to check, without the page restating their full address.
	MaskedEmail string
}

// NeedsChallenge reports whether a second factor is still outstanding.
func (r LoginResult) NeedsChallenge() bool { return r.ChallengeToken != "" }

// BeginLogin verifies the password and either issues a session or opens a
// second-factor challenge.
//
// Both surfaces (the HTML form and /api/v1/login) call this, so the factor
// policy cannot drift between them — which is also why later phases get MFA on
// passkey and OAuth sign-ins for free.
func (s *Service) BeginLogin(ctx context.Context, email, password string, info store.SessionInfo) (LoginResult, error) {
	email = normalizeEmail(email)

	wait, err := s.checkLockout(ctx, email)
	if err != nil {
		return LoginResult{}, err
	}
	if wait > 0 {
		return LoginResult{}, LockedOutError{RetryAfter: wait}
	}

	u, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		BurnPasswordTime(password)
		_ = s.recordLoginFailure(ctx, email)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		_ = s.recordLoginFailure(ctx, email)
		return LoginResult{}, ErrInvalidCredentials
	}
	if u.EmailVerifiedAt == nil {
		return LoginResult{}, ErrEmailNotVerified
	}
	if u.Disabled() {
		return LoginResult{}, ErrAccountDisabled
	}

	// The password was right, so the address is no longer under suspicion even
	// if a second factor is still outstanding — otherwise a user who fumbles
	// the code repeatedly would stay locked out by their earlier typos.
	_ = s.clearLoginFailures(ctx, email)

	factors, err := s.enabledFactors(ctx, u.ID)
	if err != nil {
		return LoginResult{}, err
	}
	if len(factors) == 0 {
		token, err := s.issueSession(ctx, u.ID, info)
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{SessionToken: token, UserID: u.ID}, nil
	}

	return s.openChallenge(ctx, u, factors, info)
}

// openChallenge creates a second-factor challenge for a user who has already
// proved a first factor.
//
// Shared by password sign-in and by an unverified passkey assertion, so both
// arrive at the same prompt with the same rules — the alternative is two
// almost-identical blocks that drift.
func (s *Service) openChallenge(ctx context.Context, u store.User, factors []Factor, info store.SessionInfo) (LoginResult, error) {
	raw, err := RandomToken()
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.store.CreateChallenge(ctx, u.ID, HashToken(raw), store.KindMFA,
		joinFactors(factors), time.Now().Add(challengeTTL), info); err != nil {
		return LoginResult{}, err
	}
	// An email-code user gets their code immediately: making them press "send"
	// on arrival is a step with no decision in it.
	if containsFactor(factors, FactorEmailOTP) && !containsFactor(factors, FactorTOTP) {
		if err := s.SendChallengeCode(ctx, raw); err != nil {
			return LoginResult{}, err
		}
	}
	return LoginResult{
		ChallengeToken: raw,
		Methods:        factors,
		UserID:         u.ID,
		MaskedEmail:    MaskEmail(u.Email),
	}, nil
}

// enabledFactors lists the second factors this user has actually turned on,
// most convenient first. Recovery codes are never offered as the primary
// prompt: they are the way out when the others are unavailable, so they are
// appended only when something else already requires a challenge.
func (s *Service) enabledFactors(ctx context.Context, userID int64) ([]Factor, error) {
	var out []Factor
	if t, err := s.store.GetTOTP(ctx, userID); err == nil && t.Confirmed() {
		out = append(out, FactorTOTP)
	} else if err != nil && !errors.Is(err, store.ErrNoTOTP) {
		return nil, err
	}
	on, err := s.store.EmailOTPEnabled(ctx, userID)
	if err != nil {
		return nil, err
	}
	if on {
		out = append(out, FactorEmailOTP)
	}
	if len(out) == 0 {
		return nil, nil
	}
	if n, err := s.store.CountUnusedRecoveryCodes(ctx, userID); err == nil && n > 0 {
		out = append(out, FactorRecovery)
	}
	return out, nil
}

// ChallengeInfo describes an open challenge to the surface rendering it.
type ChallengeInfo struct {
	Methods     []Factor
	MaskedEmail string
	CodeSent    bool
}

// LookupChallenge resolves a challenge token for rendering.
func (s *Service) LookupChallenge(ctx context.Context, rawChallenge string) (ChallengeInfo, error) {
	c, err := s.store.GetChallenge(ctx, HashToken(rawChallenge), store.KindMFA)
	if err != nil {
		return ChallengeInfo{}, ErrChallengeExpired
	}
	u, err := s.store.GetUserByID(ctx, c.UserID)
	if err != nil {
		return ChallengeInfo{}, err
	}
	return ChallengeInfo{
		Methods:     splitFactors(c.Methods),
		MaskedEmail: MaskEmail(u.Email),
		CodeSent:    c.CodeHash != "",
	}, nil
}

// SendChallengeCode emails a fresh one-time code for an open challenge.
func (s *Service) SendChallengeCode(ctx context.Context, rawChallenge string) error {
	c, err := s.store.GetChallenge(ctx, HashToken(rawChallenge), store.KindMFA)
	if err != nil {
		return ErrChallengeExpired
	}
	if !containsFactor(splitFactors(c.Methods), FactorEmailOTP) {
		return ErrNoSuchFactor
	}
	code, err := NumericCode(codeDigits)
	if err != nil {
		return err
	}
	if err := s.store.SetChallengeCode(ctx, c.TokenHash, HashToken(code), time.Now().Add(codeTTL)); err != nil {
		return err
	}
	u, err := s.store.GetUserByID(ctx, c.UserID)
	if err != nil {
		return err
	}
	msg, err := emailtpl.LoginCode(s.localeFor(ctx, u.ID), code, codeTTL)
	if err != nil {
		return err
	}
	msg.To = u.Email
	return s.mailer.Send(ctx, msg)
}

// CompleteChallenge verifies a submitted factor and issues a session.
//
// Every wrong answer costs an attempt regardless of which factor was tried, so
// the allowance cannot be stretched by alternating between them.
func (s *Service) CompleteChallenge(ctx context.Context, rawChallenge string, factor Factor, code string, info store.SessionInfo) (string, error) {
	c, err := s.store.GetChallenge(ctx, HashToken(rawChallenge), store.KindMFA)
	if err != nil {
		return "", ErrChallengeExpired
	}
	if !containsFactor(splitFactors(c.Methods), factor) {
		return "", ErrNoSuchFactor
	}

	ok, err := s.verifyFactor(ctx, c, factor, code)
	if err != nil {
		return "", err
	}
	if !ok {
		attempts, err := s.store.IncrementChallengeAttempts(ctx, c.TokenHash)
		if err != nil {
			return "", ErrChallengeExpired
		}
		if attempts >= maxChallengeAttempts {
			// Destroyed rather than merely blocked: a spent challenge that
			// lingers is a token an attacker can keep hammering once the
			// counter is reset by some future bug.
			_ = s.store.DeleteChallenge(ctx, c.TokenHash)
			return "", ErrTooManyAttempts
		}
		return "", ErrInvalidCredentials
	}

	// Consuming is the atomic step: two correct submissions racing each other
	// cannot both mint a session, because only one UPDATE can move consumed_at.
	userID, err := s.store.ConsumeChallenge(ctx, c.TokenHash, store.KindMFA)
	if err != nil {
		return "", ErrChallengeExpired
	}
	return s.issueSession(ctx, userID, info)
}

// verifyFactor checks one submitted factor without touching the challenge.
func (s *Service) verifyFactor(ctx context.Context, c store.AuthChallenge, factor Factor, code string) (bool, error) {
	switch factor {
	case FactorTOTP:
		return s.verifyTOTPCode(ctx, c.UserID, code)
	case FactorEmailOTP:
		if c.CodeHash == "" || c.CodeExpiresAt == nil || time.Now().After(*c.CodeExpiresAt) {
			return false, nil
		}
		// Constant-time even though both sides are already hashes: the
		// comparison is cheap and this is the one place a timing difference
		// would be measurable per-guess.
		return subtle.ConstantTimeCompare([]byte(c.CodeHash), []byte(HashToken(normalizeCode(code)))) == 1, nil
	case FactorRecovery:
		return s.store.ConsumeRecoveryCode(ctx, c.UserID, HashToken(normalizeCode(code)))
	default:
		return false, ErrNoSuchFactor
	}
}

// AbandonChallenge discards an open challenge (the user pressed cancel).
func (s *Service) AbandonChallenge(ctx context.Context, rawChallenge string) error {
	return s.store.DeleteChallenge(ctx, HashToken(rawChallenge))
}

// issueSession mints and stores a session token.
func (s *Service) issueSession(ctx context.Context, userID int64, info store.SessionInfo) (string, error) {
	raw, err := RandomToken()
	if err != nil {
		return "", err
	}
	if info.Label == "" {
		info.Label = DeviceLabel(info.UserAgent)
	}
	exp := time.Now().Add(s.cfg.sessionTTL())
	if err := s.store.CreateSession(ctx, userID, HashToken(raw), exp, info); err != nil {
		return "", err
	}
	return raw, nil
}

// NumericCode returns a cryptographically random decimal code, zero-padded so
// every code is exactly n digits — trimming a leading zero would leak that the
// value was small.
func NumericCode(n int) (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	return fmt.Sprintf("%0*d", n, v), nil
}

// normalizeCode strips the spaces and dashes people paste in from an email or
// a password manager, so "123 456" is the same code as "123456".
func normalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(code)))
}

// MaskEmail renders an address as "j•••n@example.com" for a screen shown to
// someone who has not finished proving who they are.
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 2 {
		return strings.Repeat("•", len(local)) + domain
	}
	return string(local[0]) + strings.Repeat("•", len(local)-2) + string(local[len(local)-1]) + domain
}

func joinFactors(fs []Factor) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = string(f)
	}
	return strings.Join(parts, ",")
}

func splitFactors(s string) []Factor {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]Factor, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, Factor(p))
		}
	}
	return out
}

func containsFactor(fs []Factor, want Factor) bool {
	for _, f := range fs {
		if f == want {
			return true
		}
	}
	return false
}
