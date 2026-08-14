package oidcauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The audience check is the whole security of accepting an ID token. Without
// it, a token Google minted for ANY other application would authenticate its
// bearer here — the signature is valid, the issuer is right, the expiry is
// fine, and only `aud` says who it was for.
func TestAudienceAllowlist(t *testing.T) {
	const (
		web     = "web.apps.googleusercontent.com"
		ios     = "ios.apps.googleusercontent.com"
		android = "android.apps.googleusercontent.com"
		foreign = "someone-elses-app.apps.googleusercontent.com"
	)
	ours := []string{web, ios, android}

	for _, tc := range []struct {
		name     string
		token    []string
		allowed  []string
		expected bool
	}{
		{"our web client", []string{web}, ours, true},
		{"our ios client", []string{ios}, ours, true},
		{"our android client", []string{android}, ours, true},
		{"another app entirely", []string{foreign}, ours, false},
		{"ours among several", []string{foreign, ios}, ours, true},
		{"none of ours", []string{foreign, "yet-another"}, ours, false},
		// An unconfigured allowlist must reject everything. Reading "nothing
		// configured" as "allow anything" would be the worst possible default.
		{"empty allowlist rejects", []string{web}, nil, false},
		{"no audience at all", nil, ours, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := audienceAllowed(tc.token, tc.allowed); got != tc.expected {
				t.Fatalf("audienceAllowed(%v, %v) = %v, want %v",
					tc.token, tc.allowed, got, tc.expected)
			}
		})
	}
}

// The web client id is always in the allowlist, so setting up the browser flow
// cannot be undone by forgetting to repeat that id under allowed_audiences.
func TestAudienceListAlwaysIncludesTheWebClient(t *testing.T) {
	got := audienceList(Config{
		ClientID:         "web.example",
		AllowedAudiences: []string{"ios.example"},
	})
	if !contains(got, "web.example") || !contains(got, "ios.example") {
		t.Fatalf("audienceList = %v, want both ids", got)
	}
	// No duplicate when it is listed explicitly as well.
	got = audienceList(Config{
		ClientID:         "web.example",
		AllowedAudiences: []string{"web.example"},
	})
	if len(got) != 1 {
		t.Fatalf("audienceList = %v, want one entry", got)
	}
}

// Apple sends email_verified as the STRING "true"; Google sends a JSON boolean.
// Reading it as a bool alone silently treats every Apple identity as
// unverified, which would block auto-linking for every Apple user.
func TestEmailVerifiedAcceptsBothShapes(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"false", false},
		{nil, false},
		{"yes", false},
		{1, false},
	} {
		if got := truthy(tc.in); got != tc.want {
			t.Errorf("truthy(%#v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// A provider with no client id is skipped rather than half-configured.
func TestRegistryOnlyHoldsConfiguredProviders(t *testing.T) {
	r := &Registry{providers: map[string]Provider{}}
	if r.Enabled() {
		t.Fatal("an empty registry is not enabled")
	}
	if _, err := r.Get(ProviderGoogle); err != ErrUnknownProvider {
		t.Fatalf("want ErrUnknownProvider, got %v", err)
	}
}

// A malformed .p8 must fail loudly at startup rather than at the first sign-in
// attempt weeks later.
func TestAppleSignerRejectsBadKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  AppleConfig
	}{
		{"not PEM", AppleConfig{PrivateKeyPEM: "definitely not a key"}},
		{"PEM but not a key", AppleConfig{
			PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----\nZGVhZGJlZWY=\n-----END PRIVATE KEY-----\n",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newAppleSigner(tc.cfg); err == nil {
				t.Fatal("a bad key must be rejected")
			}
		})
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// The minted client secret has to carry exactly the claims Apple checks. Every
// one of these is a value Apple validates server-side, so getting one wrong —
// transposing iss and sub is the easy mistake, since both are opaque
// Apple-issued strings — fails every exchange at runtime with nothing in the
// build to catch it.
func TestAppleClientSecretClaims(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	cfg := AppleConfig{
		Config:        Config{ClientID: "ca.pigglet.budget.service"},
		TeamID:        "ABCDE12345",
		KeyID:         "KEY1234567",
		PrivateKeyPEM: pemText,
	}
	sign, err := newAppleSigner(cfg)
	if err != nil {
		t.Fatalf("a valid P-256 .p8 should be accepted: %v", err)
	}
	secret, err := sign()
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := jwt.Parse(secret, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
	if err != nil {
		t.Fatalf("the secret should verify against its own key: %v", err)
	}
	if parsed.Method.Alg() != "ES256" {
		t.Errorf("alg = %q, want ES256", parsed.Method.Alg())
	}
	if kid, _ := parsed.Header["kid"].(string); kid != cfg.KeyID {
		t.Errorf("kid = %q, want %q", kid, cfg.KeyID)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims are %T", parsed.Claims)
	}
	// The team issues the assertion; the Services ID is its subject. Reversing
	// these is the mistake this test exists to catch.
	if got, _ := claims["iss"].(string); got != cfg.TeamID {
		t.Errorf("iss = %q, want the team id %q", got, cfg.TeamID)
	}
	if got, _ := claims["sub"].(string); got != cfg.ClientID {
		t.Errorf("sub = %q, want the services id %q", got, cfg.ClientID)
	}
	if got, _ := claims["aud"].(string); got != appleIssuer {
		t.Errorf("aud = %q, want %q", got, appleIssuer)
	}

	// Apple rejects anything more than six months out. Minting a short-lived
	// assertion per exchange is what keeps this from ever being a live problem,
	// so pin that the TTL stays well inside the cap.
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		t.Fatalf("no expiry claim: %v", err)
	}
	life := time.Until(exp.Time)
	if life <= 0 {
		t.Fatalf("secret is already expired (%v)", life)
	}
	if life > 6*30*24*time.Hour {
		t.Fatalf("expiry %v exceeds Apple's six-month cap", life)
	}
	if life > time.Hour {
		t.Errorf("expiry %v is longer than a per-exchange assertion needs; "+
			"short-lived is what removes the expiry class entirely", life)
	}
}
