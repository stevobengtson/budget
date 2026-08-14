package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/sbengtson/budget/internal/core/oidcauth"
	"github.com/sbengtson/budget/internal/core/store"
)

// oauthStateTTL bounds a handshake. Long enough for a consent screen and a
// password prompt at the provider, short enough that an abandoned one expires.
const oauthStateTTL = 10 * time.Minute

// oauthState is what has to survive the round trip to the provider.
//
// It lives in auth_challenges, not a cookie. Apple returns via a cross-site
// form_post, on which a SameSite=Lax cookie is simply not sent — a cookie-based
// implementation works perfectly with Google and fails only with Apple, which
// is a miserable thing to diagnose after the fact.
type oauthState struct {
	Provider string `json:"provider"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	// LinkUserID is set when the handshake is "attach this provider to my
	// account" rather than "sign me in".
	LinkUserID int64 `json:"linkUserId,omitempty"`
}

// BeginOAuth starts a handshake and returns the URL to send the browser to.
//
// linkUserID is 0 for a sign-in, or the signed-in user's id when linking from
// the security screen.
func (s *Service) BeginOAuth(ctx context.Context, providerName string, linkUserID int64, info store.SessionInfo) (string, error) {
	if s.oauth == nil {
		return "", oidcauth.ErrUnknownProvider
	}
	p, err := s.oauth.Get(providerName)
	if err != nil {
		return "", err
	}

	// The state is the challenge token itself: one random value, stored hashed,
	// that the callback must present to prove it belongs to a handshake we
	// started.
	state, err := RandomToken()
	if err != nil {
		return "", err
	}
	nonce, err := RandomToken()
	if err != nil {
		return "", err
	}
	verifier, err := RandomToken()
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(oauthState{
		Provider: providerName, Nonce: nonce, Verifier: verifier, LinkUserID: linkUserID,
	})
	if err != nil {
		return "", err
	}
	if err := s.store.CreateChallengeWithData(ctx, linkUserID, HashToken(state),
		store.KindOAuth, providerName, payload, time.Now().Add(oauthStateTTL), info); err != nil {
		return "", err
	}
	return p.AuthCodeURL(state, nonce, pkceChallenge(verifier)), nil
}

// OAuthResult reports what a completed handshake was for.
type OAuthResult struct {
	// Login is set when the handshake was a sign-in.
	Login LoginResult
	// Linked is true when the handshake attached a provider to an existing
	// signed-in account instead.
	Linked bool
	// Provider names the issuer, for the message shown afterwards.
	Provider string
}

// FinishOAuth completes a handshake from the provider's callback.
func (s *Service) FinishOAuth(ctx context.Context, state, code string, info store.SessionInfo) (OAuthResult, error) {
	if s.oauth == nil {
		return OAuthResult{}, oidcauth.ErrUnknownProvider
	}
	c, err := s.store.GetChallenge(ctx, HashToken(state), store.KindOAuth)
	if err != nil {
		return OAuthResult{}, ErrChallengeExpired
	}
	var st oauthState
	if err := json.Unmarshal(c.Data, &st); err != nil {
		return OAuthResult{}, ErrChallengeExpired
	}
	p, err := s.oauth.Get(st.Provider)
	if err != nil {
		return OAuthResult{}, err
	}

	// Consumed before the exchange: a replayed callback must not be able to run
	// a second exchange with the same code.
	if _, err := s.store.ConsumeChallenge(ctx, c.TokenHash, store.KindOAuth); err != nil {
		return OAuthResult{}, ErrChallengeExpired
	}

	id, err := p.Exchange(ctx, code, st.Verifier, st.Nonce)
	if err != nil {
		return OAuthResult{}, err
	}

	if st.LinkUserID != 0 {
		if err := s.LinkIdentityToUser(ctx, st.LinkUserID, id); err != nil {
			return OAuthResult{}, err
		}
		return OAuthResult{Linked: true, Provider: st.Provider}, nil
	}
	r, err := s.LoginWithIdentity(ctx, id, info)
	if err != nil {
		return OAuthResult{}, err
	}
	return OAuthResult{Login: r, Provider: st.Provider}, nil
}

// LoginWithIDToken is the native path: the app obtained a token from the
// platform itself, so there is no redirect and no code to exchange.
func (s *Service) LoginWithIDToken(ctx context.Context, providerName, rawIDToken, nonce string, info store.SessionInfo) (LoginResult, error) {
	if s.oauth == nil {
		return LoginResult{}, oidcauth.ErrUnknownProvider
	}
	p, err := s.oauth.Get(providerName)
	if err != nil {
		return LoginResult{}, err
	}
	id, err := p.VerifyIDToken(ctx, rawIDToken, nonce)
	if err != nil {
		return LoginResult{}, err
	}
	return s.LoginWithIdentity(ctx, id, info)
}

// OAuthProviders lists the configured providers, for rendering buttons.
func (s *Service) OAuthProviders() []string {
	if s.oauth == nil {
		return nil
	}
	return s.oauth.Names()
}

// pkceChallenge derives the S256 challenge from a verifier.
//
// PKCE binds the authorization code to this particular handshake, so a code
// intercepted in transit cannot be redeemed by anyone who did not start it.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
