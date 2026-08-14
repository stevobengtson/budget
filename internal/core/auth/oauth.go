package auth

import (
	"context"
	"database/sql"
	"errors"

	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/oidcauth"
	"github.com/sbengtson/budget/internal/core/store"
)

var (
	// ErrNoAccountForIdentity means the provider's identity does not match any
	// account, and could not be safely attached to one.
	ErrNoAccountForIdentity = errors.New("no Pigglet account matches that sign-in")
	// ErrIdentityUnverified means the provider would not vouch for the address,
	// so it cannot be used to claim an existing account.
	ErrIdentityUnverified = errors.New("that provider did not verify your email address")
	// ErrIdentityTaken means the identity is already attached to someone else.
	ErrIdentityTaken = errors.New("that account is already linked to another user")
)

// LoginWithIdentity signs in with a verified federated identity.
//
// The rules, in order:
//
//  1. Already linked — sign in.
//  2. Not linked, the provider asserts the address is verified, and it matches
//     an account whose own address is verified — link it, and say so by email.
//  3. The provider will not vouch for the address — refuse. Never link, never
//     create. Google reports false for some Workspace configurations, and an
//     unverified assertion is not evidence of controlling the mailbox.
//  4. Whatever happens above, an account with a second factor still gets a
//     challenge. Federated sign-in is a FIRST factor.
//
// Rule 2 deserves its reasoning written down, because auto-linking looks
// alarming at first glance. Both parties have independently proved control of
// the same mailbox: the provider says so, and the account's own address was
// verified here. Control of that mailbox already grants account takeover
// through the password-reset flow, so refusing to link buys no security — it
// only strands the user at a dead end with no way to explain it.
func (s *Service) LoginWithIdentity(ctx context.Context, id oidcauth.Identity, info store.SessionInfo) (LoginResult, error) {
	// 1. An existing link is the whole answer.
	existing, err := s.store.GetIdentity(ctx, id.Provider, id.Subject)
	if err == nil {
		_ = s.store.TouchIdentity(ctx, id.Provider, id.Subject, id.Email, id.EmailVerified)
		return s.finishIdentityLogin(ctx, existing.UserID, info)
	}
	if !errors.Is(err, store.ErrNoIdentity) {
		return LoginResult{}, err
	}

	// 3. Without a verified address there is nothing safe to match on.
	if !id.EmailVerified || id.Email == "" {
		return LoginResult{}, ErrIdentityUnverified
	}

	u, err := s.store.GetUserByEmail(ctx, normalizeEmail(id.Email))
	if errors.Is(err, sql.ErrNoRows) {
		// No account. Deliberately NOT created here: registration seeds a
		// starter budget in a language the first-run wizard has yet to ask for,
		// and silently making an account from a sign-in attempt would skip it.
		return LoginResult{}, ErrNoAccountForIdentity
	}
	if err != nil {
		return LoginResult{}, err
	}
	// The local address must be verified too. Matching an unverified account
	// would let someone who registered an address they do not own claim it
	// later by signing in with the provider that does.
	if u.EmailVerifiedAt == nil {
		return LoginResult{}, ErrIdentityUnverified
	}
	if u.Disabled() {
		return LoginResult{}, ErrAccountDisabled
	}

	// 2. Both sides proved the same mailbox: link.
	if err := s.store.LinkIdentity(ctx, u.ID, id.Provider, id.Subject, id.Email, id.EmailVerified); err != nil {
		return LoginResult{}, err
	}
	s.notifySecurityChange(ctx, u.ID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventIdentityLinked})
	return s.finishIdentityLogin(ctx, u.ID, info)
}

// finishIdentityLogin issues a session, or opens a challenge when the account
// has a second factor.
//
// Rule 4 lives here, and it is the reason this goes through openChallenge like
// every other first factor: a user who turned on two-step verification must not
// find that adding "Sign in with Google" quietly turned it off.
func (s *Service) finishIdentityLogin(ctx context.Context, userID int64, info store.SessionInfo) (LoginResult, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return LoginResult{}, err
	}
	if u.Disabled() {
		return LoginResult{}, ErrAccountDisabled
	}
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

// LinkIdentityToUser attaches a provider to the signed-in account, from the
// security screen. Step-up gated by the route.
func (s *Service) LinkIdentityToUser(ctx context.Context, userID int64, id oidcauth.Identity) error {
	existing, err := s.store.GetIdentity(ctx, id.Provider, id.Subject)
	if err == nil {
		if existing.UserID == userID {
			return nil // already linked here; nothing to do
		}
		return ErrIdentityTaken
	}
	if !errors.Is(err, store.ErrNoIdentity) {
		return err
	}
	if err := s.store.LinkIdentity(ctx, userID, id.Provider, id.Subject, id.Email, id.EmailVerified); err != nil {
		return err
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventIdentityLinked})
	return nil
}

// ListIdentities returns the account's linked providers.
func (s *Service) ListIdentities(ctx context.Context, userID int64) ([]store.OAuthIdentity, error) {
	return s.store.ListIdentitiesForUser(ctx, userID)
}

// UnlinkIdentity detaches a provider, refusing to strand the account.
//
// Someone who arrived through Google may have no password and no passkey.
// Removing their only way in would leave the account reachable solely by the
// password-reset flow, and only if they worked out that was the way back.
func (s *Service) UnlinkIdentity(ctx context.Context, userID, id int64) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == "" {
		passkeys, err := s.store.CountCredentials(ctx, userID)
		if err != nil {
			return err
		}
		identities, err := s.store.CountIdentities(ctx, userID)
		if err != nil {
			return err
		}
		if passkeys == 0 && identities <= 1 {
			return ErrLastFactor
		}
	}
	ok, err := s.store.DeleteIdentityForUser(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidToken
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventIdentityUnlinked})
	return nil
}
