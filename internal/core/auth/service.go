package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/mail"
	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/store"
)

// Config carries tunable durations. Zero values fall back to sane defaults.
type Config struct {
	SessionTTL time.Duration
	TokenTTL   time.Duration
	// ReauthWindow is how long a proved factor keeps sensitive actions
	// unlocked (see stepup.go).
	ReauthWindow time.Duration
	// Lockout is the password-failure escalation curve (see lockout.go).
	Lockout LockoutPolicy
}

func (c Config) reauthWindow() time.Duration {
	if c.ReauthWindow == 0 {
		return 15 * time.Minute
	}
	return c.ReauthWindow
}

func (c Config) sessionTTL() time.Duration {
	if c.SessionTTL == 0 {
		return 720 * time.Hour
	}
	return c.SessionTTL
}

func (c Config) tokenTTL() time.Duration {
	if c.TokenTTL == 0 {
		return time.Hour
	}
	return c.TokenTTL
}

// Service orchestrates the store + mailer for auth flows.
type Service struct {
	store   *store.Store
	mailer  mail.Mailer
	baseURL string
	cfg     Config
}

func NewService(s *store.Store, m mail.Mailer, baseURL string, cfg Config) *Service {
	return &Service{store: s, mailer: m, baseURL: strings.TrimRight(baseURL, "/"), cfg: cfg}
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// Register creates a user, claims pre-auth data if this is the first user, and
// emails a verification link.
func (s *Service) Register(ctx context.Context, email, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	email = normalizeEmail(email)
	if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return ErrEmailTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	userID, err := s.store.CreateUser(ctx, email, hash)
	if err != nil {
		return err
	}
	// Store the language they are reading the site in right now, before the very
	// first email goes out. Without this the verification mail — the one message
	// every user receives — would always be English, because users.locale is
	// still its column default until the first-run wizard asks. The wizard
	// overwrites this with an explicit choice later.
	if err := s.store.UpdateUserLocale(ctx, userID, i18n.LocaleFrom(ctx)); err != nil {
		return err
	}
	// The first user ever becomes the owner and inherits all pre-auth data
	// (accounts, transactions, categories including the migration-seeded global
	// Income) via ClaimOrphanData.
	//
	// The starter budget is deliberately NOT seeded here. It is seeded by the
	// first-run wizard, which asks for the interface language before it does so
	// — seeding at registration would have to guess a language, and the category
	// names it inserts immediately become the user's own editable rows, so a
	// wrong guess is not something a later language switch can undo.
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 1 {
		if err := s.store.ClaimOrphanData(ctx, userID); err != nil {
			return err
		}
	}
	return s.sendToken(ctx, userID, email, "email_verify", "/verify", emailtpl.VerifyEmail)
}

func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	userID, err := s.store.ConsumeToken(ctx, HashToken(rawToken), "email_verify")
	if errors.Is(err, store.ErrTokenInvalid) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	return s.store.SetEmailVerified(ctx, userID)
}

// Login validates credentials, requires a verified email, and returns a new raw
// session token (store its hash server-side; hand the raw value to the cookie).
//
// info records where the session came from; its Label is filled in from the
// user agent when the caller leaves it empty.
func (s *Service) Login(ctx context.Context, email, password string, info store.SessionInfo) (string, error) {
	email = normalizeEmail(email)

	// Checked before anything expensive: the point of the lock is to stop
	// spending argon2id cycles on an address already known to be under attack.
	wait, err := s.checkLockout(ctx, email)
	if err != nil {
		return "", err
	}
	if wait > 0 {
		return "", LockedOutError{RetryAfter: wait}
	}

	u, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend the same time a real verification would, so the response time
		// does not reveal whether the address is registered, and count the
		// attempt so unknown addresses lock exactly like known ones do.
		BurnPasswordTime(password)
		_ = s.recordLoginFailure(ctx, email)
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		_ = s.recordLoginFailure(ctx, email)
		return "", ErrInvalidCredentials
	}
	if u.EmailVerifiedAt == nil {
		return "", ErrEmailNotVerified
	}
	// Checked after the password so a suspended account is not revealed to
	// someone who does not already hold its credentials.
	if u.Disabled() {
		return "", ErrAccountDisabled
	}
	raw, err := RandomToken()
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(s.cfg.sessionTTL())
	if info.Label == "" {
		info.Label = DeviceLabel(info.UserAgent)
	}
	if err := s.store.CreateSession(ctx, u.ID, HashToken(raw), exp, info); err != nil {
		return "", err
	}
	// Only a completed sign-in clears the counter. Doing it any earlier would
	// let an attacker reset their own budget by interleaving valid usernames.
	_ = s.clearLoginFailures(ctx, email)
	return raw, nil
}

func (s *Service) Logout(ctx context.Context, rawToken string) error {
	return s.store.DeleteSession(ctx, HashToken(rawToken))
}

// AuthenticateSession resolves a raw cookie token to its user, rejecting expired
// sessions and suspended accounts. Used by the web RequireAuth middleware and by
// the mobile API's bearer auth, so the disabled check here covers both surfaces.
// (Suspending already deletes the user's sessions; this closes the window where
// one was issued concurrently, and covers a row disabled by hand in SQL.)
func (s *Service) AuthenticateSession(ctx context.Context, rawToken string) (store.User, error) {
	sess, err := s.store.GetSessionByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return store.User{}, ErrInvalidCredentials
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, sess.TokenHash)
		return store.User{}, ErrInvalidCredentials
	}
	u, err := s.store.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return store.User{}, err
	}
	if u.Disabled() {
		_ = s.store.DeleteSession(ctx, sess.TokenHash)
		return store.User{}, ErrAccountDisabled
	}
	return u, nil
}

// RequestPasswordReset emails a reset link. To avoid account enumeration it
// returns nil even when no user matches.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	u, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.sendToken(ctx, u.ID, email, "password_reset", "/reset", emailtpl.PasswordReset)
}

func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	userID, err := s.store.ConsumeToken(ctx, HashToken(rawToken), "password_reset")
	if errors.Is(err, store.ErrTokenInvalid) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePasswordHash(ctx, userID, hash); err != nil {
		return err
	}
	// A reset is the recovery path after a suspected compromise, so every
	// existing session goes — including the attacker's, and including the
	// mobile apps', whose bearer tokens are rows in the same table. Nothing is
	// kept: the person resetting is not necessarily signed in anywhere yet.
	if _, err := s.store.DeleteSessionsForUserExcept(ctx, userID, ""); err != nil {
		return err
	}
	// The address is back under control of someone who can prove it, so the
	// failure counter that may have locked it is no longer meaningful.
	if u, err := s.store.GetUserByID(ctx, userID); err == nil {
		_ = s.clearLoginFailures(ctx, u.Email)
	}
	return nil
}

// VerifyUserPassword returns ErrInvalidCredentials unless password matches the
// user's current password. It re-authenticates a signed-in user before a
// sensitive action (wiping data, deleting the account).
func (s *Service) VerifyUserPassword(ctx context.Context, userID int64, password string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	return nil
}

// ChangePassword rotates the password for a signed-in user and signs every
// other device out. keepSessionToken is the caller's own raw session token, so
// the device making the change stays signed in; pass "" to sign out everywhere.
func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next, keepSessionToken string) error {
	if err := ValidatePassword(next); err != nil {
		return err
	}
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := VerifyPassword(u.PasswordHash, current)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	if current == next {
		return ErrSamePassword
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePasswordHash(ctx, userID, hash); err != nil {
		return err
	}
	// Rotating a password is the standard response to "someone else may be
	// signed in as me", so it has to actually evict them. Without this the old
	// session tokens keep working and the rotation achieves nothing.
	keep := ""
	if keepSessionToken != "" {
		keep = HashToken(keepSessionToken)
	}
	if _, err := s.store.DeleteSessionsForUserExcept(ctx, userID, keep); err != nil {
		return err
	}
	// Tell the account holder after the fact, so a rotation they did not perform
	// is visible to them. Best-effort: the password has already changed, and
	// failing the request now would misreport what actually happened.
	if msg, err := emailtpl.PasswordChanged(s.localeFor(ctx, userID)); err == nil {
		msg.To = u.Email
		_ = s.mailer.Send(ctx, msg)
	}
	return nil
}

// RequestEmailChange starts a verified email change: it re-checks the user's
// password, ensures the new address is valid, different, and unused, records it
// as pending, and emails a confirmation link to the NEW address. The login email
// stays put until the link is confirmed (ConfirmEmailChange).
func (s *Service) RequestEmailChange(ctx context.Context, userID int64, newEmail, currentPassword string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := VerifyPassword(u.PasswordHash, currentPassword)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	newEmail = normalizeEmail(newEmail)
	if addr, err := netmail.ParseAddress(newEmail); err != nil || addr.Address != newEmail {
		return ErrInvalidEmail
	}
	if newEmail == u.Email {
		return ErrSameEmail
	}
	if _, err := s.store.GetUserByEmail(ctx, newEmail); err == nil {
		return ErrEmailTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.store.SetPendingEmail(ctx, userID, newEmail); err != nil {
		return err
	}
	return s.sendToken(ctx, userID, newEmail, "email_change", "/account/email/verify", emailtpl.EmailChange)
}

// ConfirmEmailChange applies a pending email change after the link sent to the
// new address is clicked. It re-checks the address is still free (someone may
// have registered it since the request) before switching the login email over.
func (s *Service) ConfirmEmailChange(ctx context.Context, rawToken string) error {
	userID, err := s.store.ConsumeToken(ctx, HashToken(rawToken), "email_change")
	if errors.Is(err, store.ErrTokenInvalid) {
		return ErrInvalidToken
	}
	if err != nil {
		return err
	}
	pending, err := s.store.GetPendingEmail(ctx, userID)
	if err != nil {
		return err
	}
	if pending == "" {
		return ErrInvalidToken
	}
	if existing, err := s.store.GetUserByEmail(ctx, pending); err == nil && existing.ID != userID {
		return ErrEmailTaken
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.store.ApplyEmailChange(ctx, userID, pending)
}

// composer builds one localized message from a link and its validity window.
// emailtpl.VerifyEmail and friends all have this shape.
type composer func(i18n.Locale, string, time.Duration) (mail.Message, error)

// sendToken creates a verification token and emails a link to baseURL+path?token=raw,
// in the recipient's language.
func (s *Service) sendToken(ctx context.Context, userID int64, email, purpose, path string, compose composer) error {
	raw, err := RandomToken()
	if err != nil {
		return err
	}
	ttl := s.cfg.tokenTTL()
	if err := s.store.CreateVerificationToken(ctx, userID, HashToken(raw), purpose, time.Now().Add(ttl)); err != nil {
		return err
	}
	link := fmt.Sprintf("%s%s?token=%s", s.baseURL, path, raw)
	msg, err := compose(s.localeFor(ctx, userID), link, ttl)
	if err != nil {
		return err
	}
	msg.To = email
	return s.mailer.Send(ctx, msg)
}

// localeFor picks the language to write to a user in: their saved preference
// when there is one, otherwise whatever the current request resolved to.
//
// The fallback matters at registration, where the row was created moments ago
// and Register has only just stored the request's locale — and for anything
// sent from a background job, where there is no request at all.
func (s *Service) localeFor(ctx context.Context, userID int64) i18n.Locale {
	if u, err := s.store.GetUserByID(ctx, userID); err == nil && u.Locale.Valid() {
		return u.Locale
	}
	return i18n.LocaleFrom(ctx)
}
