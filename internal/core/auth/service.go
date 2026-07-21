package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

// Config carries tunable durations. Zero values fall back to sane defaults.
type Config struct {
	SessionTTL time.Duration
	TokenTTL   time.Duration
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
	// The first user ever becomes the owner and inherits all pre-auth data
	// (accounts, transactions, categories including the migration-seeded global
	// Income) via ClaimOrphanData. Then every user is seeded a starter Income
	// category — SeedNewUser is idempotent, so the owner (who just claimed the
	// global Income) is a no-op and never ends up with a duplicate.
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 1 {
		if err := s.store.ClaimOrphanData(ctx, userID); err != nil {
			return err
		}
	}
	if err := s.store.SeedNewUser(ctx, userID); err != nil {
		return err
	}
	return s.sendToken(ctx, userID, email, "email_verify", "/verify",
		"Verify your Pigglet email",
		"Confirm your email to finish signing up.")
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
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (string, error) {
	email = normalizeEmail(email)
	u, err := s.store.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		return "", ErrInvalidCredentials
	}
	if u.EmailVerifiedAt == nil {
		return "", ErrEmailNotVerified
	}
	raw, err := RandomToken()
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(s.cfg.sessionTTL())
	if err := s.store.CreateSession(ctx, u.ID, HashToken(raw), exp, userAgent, ip); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Service) Logout(ctx context.Context, rawToken string) error {
	return s.store.DeleteSession(ctx, HashToken(rawToken))
}

// AuthenticateSession resolves a raw cookie token to its user, rejecting expired
// sessions. Used by the RequireAuth middleware.
func (s *Service) AuthenticateSession(ctx context.Context, rawToken string) (store.User, error) {
	sess, err := s.store.GetSessionByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return store.User{}, ErrInvalidCredentials
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, sess.TokenHash)
		return store.User{}, ErrInvalidCredentials
	}
	return s.store.GetUserByID(ctx, sess.UserID)
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
	return s.sendToken(ctx, u.ID, email, "password_reset", "/reset",
		"Reset your Budget password",
		"Use the link below to set a new password.")
}

func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
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
	return s.store.UpdatePasswordHash(ctx, userID, hash)
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := VerifyPassword(u.PasswordHash, current)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.store.UpdatePasswordHash(ctx, userID, hash)
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
	return s.sendToken(ctx, userID, newEmail, "email_change", "/account/email/verify",
		"Confirm your new Pigglet email",
		"Confirm this address to make it your new Pigglet login email.")
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

// sendToken creates a verification token and emails a link to baseURL+path?token=raw.
func (s *Service) sendToken(ctx context.Context, userID int64, email, purpose, path, subject, intro string) error {
	raw, err := RandomToken()
	if err != nil {
		return err
	}
	exp := time.Now().Add(s.cfg.tokenTTL())
	if err := s.store.CreateVerificationToken(ctx, userID, HashToken(raw), purpose, exp); err != nil {
		return err
	}
	link := fmt.Sprintf("%s%s?token=%s", s.baseURL, path, raw)
	text := fmt.Sprintf("%s\n\n%s\n\nThis link expires in %s.", intro, link, s.cfg.tokenTTL())
	html := fmt.Sprintf(`<p>%s</p><p><a href="%s">%s</a></p><p>This link expires in %s.</p>`,
		intro, link, link, s.cfg.tokenTTL())
	return s.mailer.Send(ctx, mail.Message{To: email, Subject: subject, HTML: html, Text: text})
}
