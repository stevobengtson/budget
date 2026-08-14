package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
)

// postForm posts without following redirects, optionally with extra headers.
func postWithHeaders(t *testing.T, client *http.Client, url string, form url.Values, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// noRedirect returns a client that reports redirects rather than following them.
func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func TestLoginIsRateLimitedPerIP(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)
	client := noRedirect()

	// A DIFFERENT address every time. The per-account lockout also answers 429,
	// so reusing one address would make this pass whether or not the per-IP
	// limiter works at all — the two mechanisms have to be tested apart.
	var got429 bool
	// The limit is 10 per 5 minutes; a dozen attempts must run into it.
	for i := range 12 {
		form := url.Values{"email": {fmt.Sprintf("nobody%d@example.com", i)}, "password": {"wrong-password"}}
		resp := postWithHeaders(t, client, ts.URL+"/login", form, nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			if resp.Header.Get("Retry-After") == "" {
				t.Error("a throttled response should carry Retry-After")
			}
			break
		}
	}
	if !got429 {
		t.Fatal("repeated sign-in attempts from one IP should be rate limited")
	}
}

// The limiter keys on ClientIP, which is only trustworthy because the router
// restricts which proxies may set X-Forwarded-For.
//
// This reproduces the production shape: nginx runs on the same host, is the
// only trusted proxy, and APPENDS the real client to any X-Forwarded-For the
// client supplied (proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for).
// So a client trying to escape the limiter contributes the left-hand entries
// and the real address is always rightmost. Gin must resolve to that rightmost
// untrusted entry and ignore the forged prefix — otherwise every request gets a
// fresh bucket and the limit means nothing.
//
// Note this is only sound while nginx actually appends. If it passed the header
// through untouched, the forged value WOULD be honoured, because loopback is
// trusted and nginx is on loopback.
func TestForgedForwardedForCannotResetTheBucket(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)
	client := noRedirect()

	const realClient = "198.51.100.7"
	var blocked bool
	for i := range 14 {
		// A different address each time so the per-account lockout, which also
		// answers 429, cannot be what stops us.
		form := url.Values{"email": {fmt.Sprintf("ghost%d@example.com", i)}, "password": {"wrong-password"}}
		resp := postWithHeaders(t, client, ts.URL+"/login", form, map[string]string{
			"X-Forwarded-For": fmt.Sprintf("%s, %s", forgedIP(i), realClient),
		})
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatal("a forged X-Forwarded-For prefix must not give a request a fresh rate-limit bucket")
	}
}

func forgedIP(i int) string {
	return fmt.Sprintf("203.0.113.%d", i+1)
}

func TestSecurityMutationsRequireRecentAuth(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	// Age the session past the re-auth window by clearing the timestamps it is
	// measured from. Reaching into the row keeps the test honest about what the
	// middleware actually reads, without sleeping.
	stale := time.Now().Add(-2 * time.Hour)
	if _, err := s.DB().Exec(
		`UPDATE sessions SET created_at = $1, reauth_at = NULL WHERE user_id = $2`, stale, uid); err != nil {
		t.Fatal(err)
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/password",
		url.Values{"current": {"password1"}, "next": {"a-brand-new-password"}}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stale session password change = %d, want 403", resp.StatusCode)
	}
	body := readAll(t, resp)
	// A real 4xx carrying the section is the existing inline-error contract;
	// error-flash.js swaps it in place rather than firing a toast.
	if !strings.Contains(body, "data-inline-errors") || !strings.Contains(body, `id="account-reauth"`) {
		t.Fatalf("expected the re-auth card as an inline-error fragment, got: %.300s", body)
	}

	// Proving the password again opens the window, and the change then works.
	resp2 := postWithHeaders(t, client, ts.URL+"/account/reauth", url.Values{"password": {"password1"}}, nil)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("step-up = %d, want 204", resp2.StatusCode)
	}

	resp3 := postWithHeaders(t, client, ts.URL+"/account/password",
		url.Values{"current": {"password1"}, "next": {"a-brand-new-password"}}, nil)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("password change after step-up = %d, want 200: %.300s", resp3.StatusCode, readAll(t, resp3))
	}
}

func TestStepUpRejectsWrongPassword(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	resp := postWithHeaders(t, client, ts.URL+"/account/reauth", url.Values{"password": {"not-the-password"}}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("step-up with a wrong password = %d, want 401", resp.StatusCode)
	}
}

func TestSessionsCardListsDevicesAndRevokes(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	// A second device on the same account.
	other, err := auth.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(t.Context(), uid, auth.HashToken(other), time.Now().Add(time.Hour),
		store.SessionInfo{Client: "ios", Label: "Pigglet for iPhone"}); err != nil {
		t.Fatal(err)
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/account/sessions"))
	if !strings.Contains(body, "Pigglet for iPhone") {
		t.Fatalf("sessions card should list the phone, got: %.400s", body)
	}

	sessions, err := s.ListSessionsForUser(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	var otherID int64
	for _, sess := range sessions {
		if sess.TokenHash == auth.HashToken(other) {
			otherID = sess.ID
		}
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/sessions/"+strconv.FormatInt(otherID, 10)+"/revoke", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke = %d, want 200", resp.StatusCode)
	}
	remaining, err := s.ListSessionsForUser(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("sessions after revoke = %d, want 1", len(remaining))
	}
}

// Revoking is scoped by user in the same statement that deletes, so a forged id
// belonging to somebody else cannot take effect.
func TestCannotRevokeAnotherUsersSession(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	victim, err := s.CreateUser(t.Context(), "victim@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(t.Context(), victim, "victims-token", time.Now().Add(time.Hour), store.SessionInfo{}); err != nil {
		t.Fatal(err)
	}
	victimSessions, err := s.ListSessionsForUser(t.Context(), victim)
	if err != nil {
		t.Fatal(err)
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/sessions/"+strconv.FormatInt(victimSessions[0].ID, 10)+"/revoke", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("revoking another user's session = %d, want 400", resp.StatusCode)
	}
	if _, err := s.GetSessionByTokenHash(t.Context(), "victims-token"); err != nil {
		t.Fatal("the victim's session must still exist")
	}
}

// Trusted proxies must be applied. A malformed list is a configuration error
// that has to surface loudly, because the fallback is Gin's permissive default.
func TestInvalidTrustedProxiesPanics(t *testing.T) {
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Web.TrustedProxies = []string{"not-an-ip-or-cidr"}

	defer func() {
		if recover() == nil {
			t.Fatal("a malformed trusted_proxies list must not be silently ignored")
		}
	}()
	NewServer(store.New(openTestDB(t)), cfg)
}

func TestLoginCookieClearedWithMatchingSecureFlag(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	resp := postWithHeaders(t, client, ts.URL+"/logout", nil, nil)
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == handlers.SessionCookieName && c.MaxAge >= 0 {
			t.Fatalf("logout should expire the session cookie, got MaxAge=%d", c.MaxAge)
		}
	}
}

// The Security tab renders the sessions card inline, so the page load has to
// populate it. It previously did not: AccountData carried the field but
// accountData never filled it, and the card rendered with no rows at all.
func TestSecurityTabRendersSessionsInline(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	if err := s.CreateSession(t.Context(), uid, "second-device", time.Now().Add(time.Hour),
		store.SessionInfo{Client: "android", Label: "Pigglet for Android"}); err != nil {
		t.Fatal(err)
	}

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if !strings.Contains(page, `id="account-sessions"`) {
		t.Fatal("the security tab should contain the sessions card")
	}
	if !strings.Contains(page, "Pigglet for Android") {
		t.Fatal("the sessions card must be populated on the page load, not empty")
	}
}

// Viewing the list must not require step-up: someone who suspects a compromise
// needs to see their devices, and gating that behind a password prompt puts the
// diagnosis behind the cure.
func TestViewingSessionsDoesNotRequireStepUp(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	stale := time.Now().Add(-2 * time.Hour)
	if _, err := s.DB().Exec(
		`UPDATE sessions SET created_at = $1, reauth_at = NULL WHERE user_id = $2`, stale, uid); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get(ts.URL + "/account/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listing sessions with a stale session = %d, want 200", resp.StatusCode)
	}
}
