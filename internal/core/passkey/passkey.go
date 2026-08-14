// Package passkey wraps go-webauthn for the rest of the app.
//
// It exists to contain a dependency, not to add a layer. go-webauthn is a v0.x
// library that has made breaking changes between minor releases — v0.17.0
// reworked attestation types outright — so every webauthn.* and protocol.* type
// stays behind this boundary. Callers traffic in []byte JSON, store rows, and
// plain Go values, which means a library bump is a change to one package rather
// than to handlers, views, and the auth service at once.
//
// The same reasoning drives asking for no attestation: it is the part of the
// spec this app has no use for, and precisely the part that churns.
package passkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/sbengtson/budget/internal/core/store"
)

var (
	// ErrUnavailable means passkeys are not configured on this server.
	ErrUnavailable = errors.New("passkeys are not configured")
	// ErrCeremonyExpired means the registration or sign-in ceremony is unknown
	// or timed out; the user has to start again.
	ErrCeremonyExpired = errors.New("passkey ceremony expired")
	// ErrRejected means the authenticator's response failed validation — a
	// wrong origin, a bad signature, a credential we do not know.
	ErrRejected = errors.New("passkey was not accepted")
	// ErrUnknownCredential means the assertion named a credential that is not
	// registered here.
	ErrUnknownCredential = errors.New("that passkey is not registered")
)

// Config describes the relying party.
type Config struct {
	// RPID is the registrable domain: "pigglet.ca". No scheme, no port, no
	// subdomain.
	//
	// This is a one-way door. Credentials are bound to the RP ID permanently,
	// so choosing "www.pigglet.ca" would exclude the apex forever and every
	// existing passkey would stop working if it were ever changed.
	RPID string
	// RPDisplayName is the name shown in the operating system's prompt.
	RPDisplayName string
	// Origins is the allowlist of origins an assertion may come from.
	//
	// Must include the Android app's "android:apk-key-hash:<base64url sha256 of
	// the signing certificate>", which is a completely different string form
	// from the https:// web origins — and there are usually two of them, the
	// upload key and the Play App Signing key. Omitting them makes every
	// Android assertion fail origin validation while web and iOS work
	// perfectly, which is the single most common way this goes wrong.
	//
	// iOS needs no entry: ASAuthorization reports the https origin derived from
	// the associated domain.
	Origins []string
}

// Service performs the WebAuthn ceremonies.
type Service struct {
	wa    *webauthn.WebAuthn
	store *store.Store
}

// New builds the service. A zero RPID disables passkeys rather than guessing a
// value that would permanently bind credentials to the wrong domain.
func New(st *store.Store, cfg Config) (*Service, error) {
	if cfg.RPID == "" || len(cfg.Origins) == 0 {
		return nil, ErrUnavailable
	}
	name := cfg.RPDisplayName
	if name == "" {
		name = cfg.RPID
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: name,
		RPOrigins:     cfg.Origins,
		// A budgeting app has no use for knowing which authenticator model was
		// used, and attestation is both the privacy-invasive part of the spec
		// and the part this library churns on.
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			// Preferred, NOT required: "required" rejects some Android platform
			// authenticators outright. The verification flag is recorded per
			// assertion instead, so the caller can decide what a non-verified
			// assertion is worth.
			UserVerification: protocol.VerificationPreferred,
			// Ask for a discoverable credential so the user can sign in without
			// typing an address first, but do not insist: an older security key
			// that cannot store one should still be registrable as a second
			// factor.
			ResidentKey: protocol.ResidentKeyRequirementPreferred,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn config: %w", err)
	}
	return &Service{wa: wa, store: st}, nil
}

// RegisteredCredential is the outcome of a completed registration, in terms the
// store understands.
type RegisteredCredential struct {
	CredentialID    []byte
	PublicKey       []byte
	AAGUID          []byte
	SignCount       int64
	Transports      string
	AttestationType string
	BackupEligible  bool
	BackupState     bool
}

// Assertion is the outcome of a completed sign-in.
type Assertion struct {
	UserID       int64
	CredentialID []byte
	// UserVerified reports that the authenticator checked a biometric or PIN,
	// not merely that someone touched it. It is the difference between one
	// factor and two, so the caller must branch on it rather than assume.
	UserVerified bool
	// CloneWarning reports that the credential's signature counter went
	// backwards, the documented signal that it has been duplicated.
	CloneWarning bool
	// SignCount is the counter the authenticator reported, to be stored so the
	// next assertion can be compared against it.
	SignCount uint32
}

// BeginRegistration starts enrolling a new passkey and returns the options JSON
// for navigator.credentials.create, plus the ceremony state to persist.
func (s *Service) BeginRegistration(ctx context.Context, userID int64) (options, session []byte, err error) {
	u, err := s.webAuthnUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	// Exclude what is already registered so the authenticator says "you already
	// have one of these" instead of silently making a duplicate.
	exclusions := make([]protocol.CredentialDescriptor, 0, len(u.creds))
	for _, c := range u.creds {
		exclusions = append(exclusions, descriptorFor(c))
	}
	creation, sessionData, err := s.wa.BeginRegistration(u,
		webauthn.WithExclusions(exclusions))
	if err != nil {
		return nil, nil, fmt.Errorf("begin registration: %w", err)
	}
	return marshalCeremony(creation, sessionData)
}

// FinishRegistration validates the authenticator's response.
func (s *Service) FinishRegistration(ctx context.Context, userID int64, session, body []byte) (RegisteredCredential, error) {
	u, err := s.webAuthnUser(ctx, userID)
	if err != nil {
		return RegisteredCredential{}, err
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal(session, &sd); err != nil {
		return RegisteredCredential{}, ErrCeremonyExpired
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body))
	if err != nil {
		return RegisteredCredential{}, ErrRejected
	}
	cred, err := s.wa.CreateCredential(u, sd, parsed)
	if err != nil {
		return RegisteredCredential{}, fmt.Errorf("%w: %w", ErrRejected, err)
	}
	return RegisteredCredential{
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		AAGUID:          cred.Authenticator.AAGUID,
		SignCount:       int64(cred.Authenticator.SignCount),
		Transports:      joinTransports(cred.Transport),
		AttestationType: cred.AttestationType,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
	}, nil
}

// BeginLogin starts a discoverable sign-in: the authenticator names the user,
// so nothing has to be typed first.
func (s *Service) BeginLogin(ctx context.Context) (options, session []byte, err error) {
	assertion, sessionData, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		return nil, nil, fmt.Errorf("begin login: %w", err)
	}
	return marshalCeremony(assertion, sessionData)
}

// BeginLoginFor starts a sign-in scoped to one known user, for step-up where
// the account is already established.
func (s *Service) BeginLoginFor(ctx context.Context, userID int64) (options, session []byte, err error) {
	u, err := s.webAuthnUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if len(u.creds) == 0 {
		return nil, nil, ErrUnknownCredential
	}
	assertion, sessionData, err := s.wa.BeginLogin(u)
	if err != nil {
		return nil, nil, fmt.Errorf("begin login: %w", err)
	}
	return marshalCeremony(assertion, sessionData)
}

// FinishLogin validates an assertion and reports who signed in.
func (s *Service) FinishLogin(ctx context.Context, session, body []byte) (Assertion, error) {
	var sd webauthn.SessionData
	if err := json.Unmarshal(session, &sd); err != nil {
		return Assertion{}, ErrCeremonyExpired
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		return Assertion{}, ErrRejected
	}

	var resolved *webAuthnUser
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.userByHandleOrCredential(ctx, rawID, userHandle)
		if err != nil {
			return nil, err
		}
		resolved = u
		return u, nil
	}

	// ValidatePasskeyLogin covers both shapes: a discoverable assertion that
	// names its user, and a scoped one where the session already does.
	_, cred, err := s.wa.ValidatePasskeyLogin(handler, sd, parsed)
	if err != nil {
		return Assertion{}, fmt.Errorf("%w: %w", ErrRejected, err)
	}
	if resolved == nil {
		return Assertion{}, ErrUnknownCredential
	}
	return Assertion{
		UserID:       resolved.id,
		CredentialID: cred.ID,
		UserVerified: cred.Flags.UserVerified,
		CloneWarning: cred.Authenticator.CloneWarning,
		SignCount:    cred.Authenticator.SignCount,
	}, nil
}

// userByHandleOrCredential resolves the user an assertion belongs to, by the
// handle the authenticator reports or, failing that, by the credential itself.
func (s *Service) userByHandleOrCredential(ctx context.Context, rawID, userHandle []byte) (*webAuthnUser, error) {
	if len(userHandle) > 0 {
		if u, err := s.store.GetUserByWebAuthnHandle(ctx, userHandle); err == nil {
			return s.webAuthnUser(ctx, u.ID)
		}
	}
	c, err := s.store.GetCredential(ctx, rawID)
	if err != nil {
		return nil, ErrUnknownCredential
	}
	return s.webAuthnUser(ctx, c.UserID)
}

// marshalCeremony renders the browser options and the state to persist.
func marshalCeremony(options any, sd *webauthn.SessionData) ([]byte, []byte, error) {
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal options: %w", err)
	}
	sessionJSON, err := json.Marshal(sd)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal session: %w", err)
	}
	return optionsJSON, sessionJSON, nil
}

func joinTransports(ts []protocol.AuthenticatorTransport) string {
	if len(ts) == 0 {
		return ""
	}
	out := make([]byte, 0, 16)
	for i, t := range ts {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, t...)
	}
	return string(out)
}
