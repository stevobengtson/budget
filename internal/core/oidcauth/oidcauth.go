// Package oidcauth verifies federated sign-in identities from Google and Apple.
//
// The whole security of this package rests on one thing: an ID token is only
// evidence about a person if it was minted FOR THIS APPLICATION. The `aud`
// claim is what says so, and it is checked against an explicit allowlist of our
// own client IDs. Skipping that check would mean any Google ID token, issued to
// any application in the world, would authenticate its bearer here.
//
// The allowlist is a list rather than a single value because web, iOS and
// Android are three separate client IDs for the same app, and a native sign-in
// presents whichever one the platform used.
package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	// ProviderGoogle and ProviderApple name the supported issuers.
	ProviderGoogle = "google"
	ProviderApple  = "apple"

	googleIssuer = "https://accounts.google.com"
	appleIssuer  = "https://appleid.apple.com"
)

var (
	// ErrUnknownProvider means the named provider is not configured.
	ErrUnknownProvider = errors.New("that sign-in provider is not available")
	// ErrTokenRejected means the ID token failed verification: a bad signature,
	// the wrong issuer, an expired token, a mismatched nonce, or — the one that
	// matters most — an audience that is not ours.
	ErrTokenRejected = errors.New("that sign-in could not be verified")
	// ErrNoEmail means the provider returned no address, so there is nothing to
	// match against an existing account.
	ErrNoEmail = errors.New("the provider returned no email address")
)

// Identity is a verified assertion about a person from a provider.
type Identity struct {
	Provider string
	// Subject is the provider's stable identifier. This, not the email, is what
	// a link is keyed on.
	Subject string
	Email   string
	Name    string
	// EmailVerified is the provider's own claim about the address. It is
	// carried through rather than assumed: Google reports false for some
	// Workspace configurations, and Apple's relay addresses are verified but
	// disposable.
	EmailVerified bool
}

// Provider performs one issuer's half of the handshake.
type Provider interface {
	Name() string
	// AuthCodeURL is where the browser is sent to begin.
	AuthCodeURL(state, nonce, pkceChallenge string) string
	// Exchange turns an authorization code into a verified identity.
	Exchange(ctx context.Context, code, pkceVerifier, nonce string) (Identity, error)
	// VerifyIDToken checks a token a native app obtained directly, which is the
	// mobile path — the app talks to the platform, not to us.
	VerifyIDToken(ctx context.Context, rawIDToken, nonce string) (Identity, error)
}

// Config describes one provider.
type Config struct {
	// ClientID is the web client id, used for the browser handshake.
	ClientID string
	// ClientSecret is Google's static secret. Apple ignores it and mints a
	// signed assertion instead (see AppleConfig).
	ClientSecret string
	// RedirectURL is where the provider returns the browser.
	RedirectURL string
	// AllowedAudiences are every client id that may appear in an ID token we
	// accept: the web client plus the iOS and Android ones.
	//
	// This is the single most security-relevant field in the package. An empty
	// or over-broad list means tokens minted for other applications are
	// accepted as proof of identity here.
	AllowedAudiences []string
}

// AppleConfig adds what Apple needs to mint its client secret.
type AppleConfig struct {
	Config
	// TeamID is the Apple Developer team, used as the JWT issuer.
	TeamID string
	// KeyID identifies the private key.
	KeyID string
	// PrivateKeyPEM is the contents of the .p8 file.
	PrivateKeyPEM string
}

// provider is the shared implementation.
type provider struct {
	name     string
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	audience []string
	// secretFn mints the client secret for each exchange. Google returns a
	// fixed string; Apple signs a short-lived assertion.
	secretFn func() (string, error)
}

func (p *provider) Name() string { return p.name }

// New builds the configured providers. A provider with no client id is skipped
// rather than half-configured.
func New(ctx context.Context, google Config, apple AppleConfig) (*Registry, error) {
	r := &Registry{providers: map[string]Provider{}}

	if google.ClientID != "" {
		p, err := newGoogle(ctx, google)
		if err != nil {
			return nil, fmt.Errorf("google: %w", err)
		}
		r.providers[ProviderGoogle] = p
	}
	if apple.ClientID != "" {
		p, err := newApple(ctx, apple)
		if err != nil {
			return nil, fmt.Errorf("apple: %w", err)
		}
		r.providers[ProviderApple] = p
	}
	return r, nil
}

// Registry holds the configured providers.
type Registry struct {
	providers map[string]Provider
}

// Get returns a provider by name.
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, ErrUnknownProvider
	}
	return p, nil
}

// Names lists the configured providers, for rendering buttons.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.providers))
	for name := range r.providers {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Enabled reports whether any provider is configured.
func (r *Registry) Enabled() bool { return len(r.providers) > 0 }

// AuthCodeURL sends the browser to the provider, carrying the state, a nonce
// bound to the ID token, and a PKCE challenge.
func (p *provider) AuthCodeURL(state, nonce, pkceChallenge string) string {
	opts := []oauth2.AuthCodeOption{
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	if p.name == ProviderApple {
		// Apple returns the result as a cross-site POST when any scope is
		// requested, and will not release name or email otherwise.
		opts = append(opts,
			oauth2.SetAuthURLParam("response_mode", "form_post"))
	}
	return p.oauth.AuthCodeURL(state, opts...)
}

// Exchange turns an authorization code into a verified identity.
func (p *provider) Exchange(ctx context.Context, code, pkceVerifier, nonce string) (Identity, error) {
	secret, err := p.secretFn()
	if err != nil {
		return Identity{}, fmt.Errorf("client secret: %w", err)
	}
	cfg := *p.oauth
	cfg.ClientSecret = secret

	token, err := cfg.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pkceVerifier))
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrTokenRejected, err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, ErrTokenRejected
	}
	return p.VerifyIDToken(ctx, raw, nonce)
}

// VerifyIDToken checks signature, issuer, expiry, audience and nonce.
func (p *provider) VerifyIDToken(ctx context.Context, rawIDToken, nonce string) (Identity, error) {
	tok, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrTokenRejected, err)
	}
	// The verifier is configured with SkipClientIDCheck so that the audience
	// can be matched against the whole allowlist rather than one value. That
	// makes this check mandatory, not belt-and-braces: without it any token
	// from this issuer, for any application, would be accepted.
	if !audienceAllowed(tok.Audience, p.audience) {
		return Identity{}, fmt.Errorf("%w: audience %v is not ours", ErrTokenRejected, tok.Audience)
	}
	if nonce != "" && tok.Nonce != nonce {
		return Identity{}, fmt.Errorf("%w: nonce mismatch", ErrTokenRejected)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := tok.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrTokenRejected, err)
	}
	return Identity{
		Provider:      p.name,
		Subject:       tok.Subject,
		Email:         strings.ToLower(strings.TrimSpace(claims.Email)),
		Name:          claims.Name,
		EmailVerified: truthy(claims.EmailVerified),
	}, nil
}

// audienceAllowed reports whether any audience in the token is one of ours.
func audienceAllowed(tokenAudience, allowed []string) bool {
	if len(allowed) == 0 {
		// Fail closed. An empty allowlist means nothing was configured, and
		// accepting everything would be the worst possible reading of that.
		return false
	}
	for _, a := range tokenAudience {
		if slices.Contains(allowed, a) {
			return true
		}
	}
	return false
}

// truthy normalizes email_verified, which Apple sends as the STRING "true"
// while Google sends a JSON boolean. Reading it as a bool alone silently
// treats every Apple identity as unverified.
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	default:
		return false
	}
}

// newGoogle builds the Google provider.
func newGoogle(ctx context.Context, cfg Config) (*provider, error) {
	p, err := oidc.NewProvider(ctx, googleIssuer)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	secret := cfg.ClientSecret
	return &provider{
		name: ProviderGoogle,
		// SkipClientIDCheck because the audience is validated against the full
		// allowlist in VerifyIDToken — the library only compares one value.
		verifier: p.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: secret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		audience: audienceList(cfg),
		secretFn: func() (string, error) { return secret, nil },
	}, nil
}

// newApple builds the Apple provider.
//
// Apple has no static client secret: it takes a short-lived ES256 assertion
// signed with a .p8 key, which is minted per exchange. Apple rejects one whose
// expiry is more than six months out; this uses minutes, so there is no
// long-lived secret to rotate or leak.
func newApple(ctx context.Context, cfg AppleConfig) (*provider, error) {
	p, err := oidc.NewProvider(ctx, appleIssuer)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	signer, err := newAppleSigner(cfg)
	if err != nil {
		return nil, err
	}
	return &provider{
		name:     ProviderApple,
		verifier: p.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		oauth: &oauth2.Config{
			ClientID:    cfg.ClientID,
			RedirectURL: cfg.RedirectURL,
			Endpoint:    p.Endpoint(),
			// Apple releases name and email only with these scopes, and only
			// on the first authorization.
			Scopes: []string{oidc.ScopeOpenID, "email", "name"},
		},
		audience: audienceList(cfg.Config),
		secretFn: signer,
	}, nil
}

// audienceList returns the allowlist, always including the web client id so a
// misconfiguration cannot lock out the browser flow that was just set up.
func audienceList(cfg Config) []string {
	out := make([]string, 0, len(cfg.AllowedAudiences)+1)
	out = append(out, cfg.AllowedAudiences...)
	if cfg.ClientID != "" && !slices.Contains(out, cfg.ClientID) {
		out = append(out, cfg.ClientID)
	}
	return out
}

// appleSecretTTL is how long a minted Apple client secret is valid. Apple caps
// this at six months; minutes is plenty, since one is minted per exchange.
const appleSecretTTL = 5 * time.Minute
