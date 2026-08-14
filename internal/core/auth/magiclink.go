package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/store"
)

// magicLinkTTL is how long a sign-in link stays usable. Short, because the
// window between asking for one and reading the email is measured in seconds,
// and anything longer is a credential sitting in an inbox.
const magicLinkTTL = 15 * time.Minute

// RequestMagicLink emails a sign-in link and a matching code.
//
// It returns nil for an address that is not registered, so the caller renders
// exactly the same page either way. Anything that distinguishes the two turns
// this endpoint into a way to test whether an address has an account —
// RequestPasswordReset takes the same position for the same reason.
//
// The one row carries both a link and a code, drawn independently. The code is
// never a truncation of the link token: that would make the shorter value a
// weakening of the longer one rather than an alternative to it.
func (s *Service) RequestMagicLink(ctx context.Context, email string, info store.SessionInfo) error {
	email = normalizeEmail(email)

	u, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// An unverified or suspended account gets nothing, silently. Sending a
	// working sign-in link to an address nobody has proved they own would make
	// this a way around email verification entirely.
	if u.EmailVerifiedAt == nil || u.Disabled() {
		return nil
	}

	raw, err := RandomToken()
	if err != nil {
		return err
	}
	code, err := NumericCode(codeDigits)
	if err != nil {
		return err
	}
	expires := time.Now().Add(magicLinkTTL)
	if err := s.store.CreateChallenge(ctx, u.ID, HashToken(raw), store.KindLogin,
		"", expires, info); err != nil {
		return err
	}
	if err := s.store.SetChallengeCode(ctx, HashToken(raw), HashToken(code), expires); err != nil {
		return err
	}

	link := s.magicLinkURL(raw, code)
	msg, err := emailtpl.MagicLink(s.localeFor(ctx, u.ID), link, code, magicLinkTTL)
	if err != nil {
		return err
	}
	msg.To = u.Email
	return s.mailer.Send(ctx, msg)
}

// magicLinkURL builds the address in the email. Both halves travel in the URL
// so that clicking it needs nothing typed; the interstitial the link lands on
// is what actually completes the sign-in.
func (s *Service) magicLinkURL(rawChallenge, code string) string {
	return fmt.Sprintf("%s/login/magic?c=%s&k=%s",
		s.baseURL, url.QueryEscape(rawChallenge), url.QueryEscape(code))
}

// LookupMagicLink validates a link without consuming it, so the interstitial
// can tell an expired link from a live one before the user presses anything.
func (s *Service) LookupMagicLink(ctx context.Context, rawChallenge string) error {
	if _, err := s.store.GetChallenge(ctx, HashToken(rawChallenge), store.KindLogin); err != nil {
		return ErrChallengeExpired
	}
	return nil
}

// ConfirmMagicLink completes a link sign-in.
//
// A magic link is a FIRST factor: it proves control of the inbox and nothing
// else. An account with two-step verification still has to clear it, which
// falls out of sharing openChallenge with the password path — otherwise adding
// this route would have quietly become a way around 2FA.
func (s *Service) ConfirmMagicLink(ctx context.Context, rawChallenge, code string, info store.SessionInfo) (LoginResult, error) {
	c, err := s.store.GetChallenge(ctx, HashToken(rawChallenge), store.KindLogin)
	if err != nil {
		return LoginResult{}, ErrChallengeExpired
	}
	if c.CodeHash == "" || c.CodeExpiresAt == nil || time.Now().After(*c.CodeExpiresAt) {
		return LoginResult{}, ErrChallengeExpired
	}
	if subtle.ConstantTimeCompare([]byte(c.CodeHash), []byte(HashToken(normalizeCode(code)))) != 1 {
		attempts, err := s.store.IncrementChallengeAttempts(ctx, c.TokenHash)
		if err != nil {
			return LoginResult{}, ErrChallengeExpired
		}
		if attempts >= maxChallengeAttempts {
			_ = s.store.DeleteChallenge(ctx, c.TokenHash)
			return LoginResult{}, ErrTooManyAttempts
		}
		return LoginResult{}, ErrInvalidCredentials
	}

	// Consumed before anything is issued: two submissions racing each other
	// cannot both mint a session.
	userID, err := s.store.ConsumeChallenge(ctx, c.TokenHash, store.KindLogin)
	if err != nil {
		return LoginResult{}, ErrChallengeExpired
	}
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return LoginResult{}, err
	}
	if u.Disabled() {
		return LoginResult{}, ErrAccountDisabled
	}
	// Reading the inbox is now proved, so the address is no longer suspect.
	_ = s.clearLoginFailures(ctx, u.Email)

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
