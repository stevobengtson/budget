package web

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/store"
)

// With nothing configured, the sign-in page offers no providers and the card
// says why rather than showing dead buttons.
func TestOAuthOffWhenUnconfigured(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	login := readAll(t, mustGetOK(t, client, ts.URL+"/login"))
	if strings.Contains(login, "/auth/google/start") {
		t.Error("no provider is configured, so no button should appear")
	}

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if !strings.Contains(page, `id="account-identities"`) {
		t.Error("the linked-accounts card should still render")
	}
	if !strings.Contains(page, "No sign-in providers are configured") {
		t.Errorf("expected an explanation, got: %.400s", page)
	}
}

// An unknown provider must not 500 or leak anything about what is configured.
func TestUnknownProviderIsRejected(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	resp, err := noRedirect().Get(ts.URL + "/auth/nonsense/start")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not available") {
		t.Errorf("expected a plain refusal, got: %.300s", body)
	}
}

// The callback accepts POST as well as GET. Apple returns a cross-site
// form_post whenever a scope is requested — and it will not release the email
// otherwise — so a GET-only callback works with Google and fails only with
// Apple.
func TestCallbackAcceptsBothMethods(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)
	client := noRedirect()

	// Neither will succeed without configured providers; what matters here is
	// that the POST is ROUTED rather than answered with 404 Not Found or 405
	// Method Not Allowed.
	get, err := client.Get(ts.URL + "/auth/apple/callback?state=x&code=y")
	if err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if get.StatusCode == http.StatusNotFound || get.StatusCode == http.StatusMethodNotAllowed {
		t.Fatalf("GET callback = %d, should be routed", get.StatusCode)
	}

	post := postWithHeaders(t, client, ts.URL+"/auth/apple/callback",
		url.Values{"state": {"x"}, "code": {"y"}}, nil)
	post.Body.Close()
	if post.StatusCode == http.StatusNotFound || post.StatusCode == http.StatusMethodNotAllowed {
		t.Fatalf("POST callback = %d — Apple's form_post must be routed", post.StatusCode)
	}
}

// A user who pressed Cancel at the provider is not an error worth alarming
// them about.
func TestProviderRefusalReturnsToSignIn(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	resp, err := noRedirect().Get(ts.URL + "/auth/google/callback?error=access_denied&state=x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect back to sign-in", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}
}

// A forged or stale state must not complete a handshake.
func TestCallbackWithUnknownStateIsRejected(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	resp, err := noRedirect().Get(ts.URL + "/auth/google/callback?state=forged&code=whatever")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("an unknown state must not sign anyone in")
	}
	for _, c := range resp.Cookies() {
		if c.Name == "budget_session" && c.Value != "" {
			t.Fatal("no session may be issued for a forged state")
		}
	}
}

// The API rejects a malformed native sign-in without reaching verification.
func TestAPIOAuthRequiresProviderAndToken(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	for _, body := range []string{`{}`, `{"provider":"google"}`, `{"idToken":"x"}`} {
		resp, err := noRedirect().Post(ts.URL+"/api/v1/login/oauth", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %s = %d, want 400", body, resp.StatusCode)
		}
	}
}

// Unlinking is a security mutation, so it sits behind the step-up gate like the
// rest of the security screen.
func TestUnlinkRequiresRecentAuth(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	if _, err := s.DB().Exec(
		`UPDATE sessions SET created_at = CURRENT_TIMESTAMP - interval '2 hours', reauth_at = NULL
		 WHERE user_id = $1`, uid); err != nil {
		t.Fatal(err)
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/identities/unlink/1", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}
