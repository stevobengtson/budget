package auth

import (
	"context"
	"errors"
	"time"

	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/store"
)

// FactorPasskey is a WebAuthn credential.
const FactorPasskey Factor = "passkey"

// ceremonyTTL bounds a half-finished WebAuthn exchange. Shorter than an MFA
// challenge because the two round trips happen back to back — the only gap is
// the user touching their authenticator.
const ceremonyTTL = 5 * time.Minute

// ErrPasskeysUnavailable means the server has no relying-party configuration.
var ErrPasskeysUnavailable = errors.New("passkeys are not configured")

// PasskeysAvailable reports whether passkey ceremonies can run at all.
func (s *Service) PasskeysAvailable() bool { return s.passkeys != nil }

// BeginPasskeyLogin starts a discoverable sign-in and returns the ceremony
// token plus the options JSON for navigator.credentials.get.
func (s *Service) BeginPasskeyLogin(ctx context.Context, info store.SessionInfo) (string, []byte, error) {
	if s.passkeys == nil {
		return "", nil, ErrPasskeysUnavailable
	}
	options, session, err := s.passkeys.BeginLogin(ctx)
	if err != nil {
		return "", nil, err
	}
	raw, err := RandomToken()
	if err != nil {
		return "", nil, err
	}
	// user_id is left unset: a discoverable sign-in does not know who is
	// signing in until the authenticator answers.
	if err := s.store.CreateChallengeWithData(ctx, 0, HashToken(raw),
		store.KindWebAuthnLogin, "", session, time.Now().Add(ceremonyTTL), info); err != nil {
		return "", nil, err
	}
	return raw, options, nil
}

// FinishPasskeyLogin validates the assertion and either issues a session or
// opens a second-factor challenge.
//
// The deciding question is whether the authenticator verified the user — a
// biometric or PIN — rather than merely confirming someone was present. A
// verified assertion is two factors in one gesture (the device, plus the thing
// that unlocked it), so it stands alone. An unverified one proves possession
// only, which is exactly what a password proves for someone who wrote it down,
// so it still has to clear whatever second factor the account requires.
func (s *Service) FinishPasskeyLogin(ctx context.Context, rawCeremony string, body []byte, info store.SessionInfo) (LoginResult, error) {
	if s.passkeys == nil {
		return LoginResult{}, ErrPasskeysUnavailable
	}
	c, err := s.store.GetChallenge(ctx, HashToken(rawCeremony), store.KindWebAuthnLogin)
	if err != nil {
		return LoginResult{}, ErrChallengeExpired
	}
	assertion, err := s.passkeys.FinishLogin(ctx, c.Data, body)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	// Consumed before anything is issued, so a replayed response cannot mint a
	// second session.
	if _, err := s.store.ConsumeChallenge(ctx, c.TokenHash, store.KindWebAuthnLogin); err != nil {
		return LoginResult{}, ErrChallengeExpired
	}

	u, err := s.store.GetUserByID(ctx, assertion.UserID)
	if err != nil {
		return LoginResult{}, err
	}
	if u.Disabled() {
		return LoginResult{}, ErrAccountDisabled
	}
	_ = s.store.RecordCredentialUse(ctx, assertion.CredentialID,
		int64(assertion.SignCount), assertion.CloneWarning)

	if assertion.UserVerified {
		token, err := s.issueSession(ctx, u.ID, info)
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{SessionToken: token, UserID: u.ID}, nil
	}

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

// BeginPasskeyRegistration starts enrolling a passkey for a signed-in user.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, userID int64, info store.SessionInfo) (string, []byte, error) {
	if s.passkeys == nil {
		return "", nil, ErrPasskeysUnavailable
	}
	options, session, err := s.passkeys.BeginRegistration(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	raw, err := RandomToken()
	if err != nil {
		return "", nil, err
	}
	if err := s.store.CreateChallengeWithData(ctx, userID, HashToken(raw),
		store.KindWebAuthnRegister, "", session, time.Now().Add(ceremonyTTL), info); err != nil {
		return "", nil, err
	}
	return raw, options, nil
}

// FinishPasskeyRegistration stores a newly enrolled passkey.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, userID int64, rawCeremony string, body []byte, name string) error {
	if s.passkeys == nil {
		return ErrPasskeysUnavailable
	}
	c, err := s.store.GetChallenge(ctx, HashToken(rawCeremony), store.KindWebAuthnRegister)
	if err != nil {
		return ErrChallengeExpired
	}
	// Scoped to the caller: a ceremony started for one account must not be
	// completable against another.
	if c.UserID != userID {
		return ErrChallengeExpired
	}
	reg, err := s.passkeys.FinishRegistration(ctx, userID, c.Data, body)
	if err != nil {
		return ErrInvalidCredentials
	}
	if _, err := s.store.ConsumeChallenge(ctx, c.TokenHash, store.KindWebAuthnRegister); err != nil {
		return ErrChallengeExpired
	}
	if name == "" {
		name = DeviceLabel(c.UserAgent)
	}
	if err := s.store.CreateCredential(ctx, store.WebAuthnCredential{
		UserID:          userID,
		CredentialID:    reg.CredentialID,
		PublicKey:       reg.PublicKey,
		AAGUID:          reg.AAGUID,
		SignCount:       reg.SignCount,
		Transports:      reg.Transports,
		AttestationType: reg.AttestationType,
		BackupEligible:  reg.BackupEligible,
		BackupState:     reg.BackupState,
		Name:            name,
	}); err != nil {
		return err
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventPasskeyAdded})
	return nil
}

// ListPasskeys returns the user's registered credentials.
func (s *Service) ListPasskeys(ctx context.Context, userID int64) ([]store.WebAuthnCredential, error) {
	return s.store.ListCredentialsForUser(ctx, userID)
}

// RenamePasskey relabels one of the user's passkeys.
func (s *Service) RenamePasskey(ctx context.Context, userID, id int64, name string) error {
	ok, err := s.store.RenameCredential(ctx, userID, id, name)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidToken
	}
	return nil
}

// DeletePasskey removes a credential, refusing to strand an account.
//
// Someone who signed up with a passkey may have no password at all. Removing
// their last one would leave nothing that can sign in — recoverable only by
// email, and only if they work out that is the way back.
func (s *Service) DeletePasskey(ctx context.Context, userID, id int64) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == "" {
		n, err := s.store.CountCredentials(ctx, userID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastFactor
		}
	}
	ok, err := s.store.DeleteCredentialForUser(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidToken
	}
	s.notifySecurityChange(ctx, userID, emailtpl.SecurityEvent{Kind: emailtpl.SecurityEventPasskeyRemoved})
	return nil
}

// passkeyCount reports how many credentials the user has, for the factor list.
func (s *Service) passkeyCount(ctx context.Context, userID int64) int {
	n, err := s.store.CountCredentials(ctx, userID)
	if err != nil {
		return 0
	}
	return n
}
