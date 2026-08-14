package oidcauth

import (
	"testing"
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
