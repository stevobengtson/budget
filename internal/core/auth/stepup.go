package auth

import (
	"context"
	"time"
)

// Factor is a way of proving who you are. Only the password exists in this
// phase; TOTP, email codes, recovery codes and passkeys join it later, and
// AvailableStepUpFactors is the single place that has to learn about each.
type Factor string

const (
	FactorPassword Factor = "password"
)

// StepUpRequired reports whether the session must re-prove a factor before a
// sensitive action is allowed.
//
// The window is per-session rather than per-action: proving yourself once
// unlocks the whole security screen for a few minutes, instead of asking for a
// password between every click. An unrecognised session is treated as requiring
// step-up rather than as an error, because "cannot tell" and "not recently
// authenticated" call for exactly the same response.
func (s *Service) StepUpRequired(ctx context.Context, rawSessionToken string) bool {
	if rawSessionToken == "" {
		return true
	}
	sess, err := s.SessionFor(ctx, rawSessionToken)
	if err != nil {
		return true
	}
	return time.Since(sess.AuthenticatedAt()) > s.cfg.reauthWindow()
}

// MarkReauthenticated opens the step-up window for this session.
func (s *Service) MarkReauthenticated(ctx context.Context, rawSessionToken string) error {
	return s.store.MarkSessionReauthenticated(ctx, HashToken(rawSessionToken))
}

// AvailableStepUpFactors lists the ways this user can re-prove themselves.
//
// It returns a list rather than a single factor because from the next phase
// onward the answer genuinely varies per user: someone who signed up with a
// passkey or with Google may have no password at all, and asking them for one
// would be an unanswerable question. Email codes become the universal fallback
// then — every account has a verified address by construction, since Login
// refuses to issue a session without one.
func (s *Service) AvailableStepUpFactors(ctx context.Context, userID int64) ([]Factor, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []Factor
	if u.PasswordHash != "" {
		out = append(out, FactorPassword)
	}
	// A registered passkey re-proves the user in one gesture, and is the only
	// answerable prompt for someone who signed up without a password.
	if s.PasskeysAvailable() && s.passkeyCount(ctx, userID) > 0 {
		out = append(out, FactorPasskey)
	}
	// Everything the user could use at sign-in also re-proves them here, which
	// is what keeps the security screen reachable for someone with no password.
	second, err := s.enabledFactors(ctx, userID)
	if err != nil {
		return nil, err
	}
	out = append(out, second...)
	return out, nil
}

// StepUpWithFactor re-proves a second factor and opens the window. It is the
// path for accounts where a password prompt is not an answerable question.
func (s *Service) StepUpWithFactor(ctx context.Context, userID int64, rawSessionToken string, factor Factor, code string) error {
	var (
		ok  bool
		err error
	)
	switch factor {
	case FactorTOTP:
		ok, err = s.verifyTOTPCode(ctx, userID, code)
	case FactorRecovery:
		ok, err = s.store.ConsumeRecoveryCode(ctx, userID, HashToken(normalizeCode(code)))
	default:
		return ErrNoSuchFactor
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	return s.MarkReauthenticated(ctx, rawSessionToken)
}

// StepUpWithPassword re-proves the password and opens the window.
func (s *Service) StepUpWithPassword(ctx context.Context, userID int64, rawSessionToken, password string) error {
	if err := s.VerifyUserPassword(ctx, userID, password); err != nil {
		return err
	}
	return s.MarkReauthenticated(ctx, rawSessionToken)
}
