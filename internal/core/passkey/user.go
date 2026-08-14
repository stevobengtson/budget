package passkey

import (
	"context"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/sbengtson/budget/internal/core/store"
)

// webAuthnUser adapts an account to the library's User interface.
//
// Unexported on purpose: it is the only place a webauthn.Credential exists
// outside this package's own functions, and letting it escape would put the
// library's types back into callers' signatures.
type webAuthnUser struct {
	id      int64
	handle  []byte
	email   string
	name    string
	creds   []store.WebAuthnCredential
	waCreds []webauthn.Credential
}

// WebAuthnID is the stable, opaque handle written into the authenticator.
func (u *webAuthnUser) WebAuthnID() []byte { return u.handle }

// WebAuthnName is what the OS credential manager lists the account under. The
// email is right here: it is what the user recognises when choosing between
// saved passkeys.
func (u *webAuthnUser) WebAuthnName() string { return u.email }

func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u.name != "" {
		return u.name
	}
	return u.email
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.waCreds }

// webAuthnUser loads an account and its credentials in the library's shape.
func (s *Service) webAuthnUser(ctx context.Context, userID int64) (*webAuthnUser, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	handle, err := s.store.EnsureWebAuthnHandle(ctx, userID)
	if err != nil {
		return nil, err
	}
	creds, err := s.store.ListCredentialsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &webAuthnUser{
		id:      u.ID,
		handle:  handle,
		email:   u.Email,
		name:    u.Name,
		creds:   creds,
		waCreds: make([]webauthn.Credential, 0, len(creds)),
	}
	for _, c := range creds {
		out.waCreds = append(out.waCreds, toWebAuthnCredential(c))
	}
	return out, nil
}

// toWebAuthnCredential converts a stored row into the library's struct.
func toWebAuthnCredential(c store.WebAuthnCredential) webauthn.Credential {
	return webauthn.Credential{
		ID:              c.CredentialID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       splitTransports(c.Transports),
		Flags: webauthn.CredentialFlags{
			BackupEligible: c.BackupEligible,
			BackupState:    c.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID: c.AAGUID,
			// Stored as int64 because Postgres has no unsigned integer type;
			// the protocol counter is a uint32 and cannot exceed its range.
			SignCount:    uint32(c.SignCount),
			CloneWarning: c.CloneWarning,
		},
	}
}

func splitTransports(s string) []protocol.AuthenticatorTransport {
	if s == "" {
		return nil
	}
	var out []protocol.AuthenticatorTransport
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, protocol.AuthenticatorTransport(s[start:i]))
			}
			start = i + 1
		}
	}
	return out
}

// descriptorFor renders a stored credential as the exclusion/allow-list entry
// the browser expects.
func descriptorFor(c store.WebAuthnCredential) protocol.CredentialDescriptor {
	wc := toWebAuthnCredential(c)
	return wc.Descriptor()
}
