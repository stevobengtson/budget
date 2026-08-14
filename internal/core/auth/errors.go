package auth

import (
	"errors"
	"time"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email not verified")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrInvalidEmail       = errors.New("enter a valid email address")
	ErrSameEmail          = errors.New("that is already your email")
	ErrAccountDisabled    = errors.New("this account has been disabled")
	ErrReauthRequired     = errors.New("re-authentication required")
	// ErrSealerUnavailable means no encryption key is configured, so TOTP
	// secrets cannot be stored safely. Enrolment refuses rather than falling
	// back to plaintext.
	ErrSealerUnavailable = errors.New("encryption key is not configured")
	// ErrSealedSecretUnreadable means a stored secret will not decrypt — almost
	// always a rotated or lost encryption key.
	ErrSealedSecretUnreadable = errors.New("stored secret cannot be decrypted")
	// ErrAlreadyEnrolled rejects confirming an enrolment that is already active.
	ErrAlreadyEnrolled = errors.New("already set up")
	// ErrSamePassword rejects a "change" that changes nothing — usually a sign
	// the user believes they have rotated a credential when they have not.
	ErrSamePassword = errors.New("new password must differ from the current one")
)

// LockedOutError means too many password attempts have been made against this
// address recently. It carries how long the caller must wait so the surface can
// decide whether to say so — see the handler for that policy choice.
type LockedOutError struct {
	RetryAfter time.Duration
}

func (e LockedOutError) Error() string { return "too many attempts" }

// LockedOut extracts the wait from err when it is a lockout, so callers can
// branch without a type switch at every site.
func LockedOut(err error) (time.Duration, bool) {
	var e LockedOutError
	if errors.As(err, &e) {
		return e.RetryAfter, true
	}
	return 0, false
}
