package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

// capMail records the last message so a test can read the emailed link.
type capMail struct{ last mail.Message }

func (c *capMail) Send(_ context.Context, m mail.Message) error {
	c.last = m
	return nil
}

// magicFixture starts a server and returns a second auth service, sharing the
// same database, whose mailer the test can read.
//
// The server's own service does the confirming (that is the code under test);
// this one only issues the link, because the running server's mailer writes to
// the console where a test cannot get at it.
func magicFixture(t *testing.T) (*httptest.Server, *store.Store, *auth.Service, *capMail) {
	t.Helper()
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	sealer, err := crypto.NewSealer("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	m := &capMail{}
	svc := auth.NewService(s, m, ts.URL, auth.Config{Sealer: sealer})
	return ts, s, svc, m
}

// verifiedUser creates an account that can receive a sign-in link.
func verifiedUser(t *testing.T, s *store.Store, email string) int64 {
	t.Helper()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.CreateUser(t.Context(), email, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmailVerified(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOnboarded(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	return uid
}

// magicParts pulls the two query parameters out of the emailed link.
func magicParts(t *testing.T, body string) (challenge, code string) {
	t.Helper()
	i := strings.Index(body, "/login/magic?")
	if i < 0 {
		t.Fatalf("no magic link in body: %q", body)
	}
	rest := body[i:]
	if j := strings.IndexAny(rest, " \n\t\"<"); j >= 0 {
		rest = rest[:j]
	}
	u, err := url.Parse(rest)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("c"), u.Query().Get("k")
}

// THE test for this phase. Corporate mail scanners — Outlook Safe Links,
// security proxies, link previewers — fetch every URL in an incoming message. A
// link that authenticated on GET would be spent by a robot before its recipient
// saw it, and would hand a session to whatever fetched it.
func TestMagicLinkGETDoesNotAuthenticate(t *testing.T) {
	ts, s, svc, m := magicFixture(t)
	verifiedUser(t, s, "scanner@example.com")

	if err := svc.RequestMagicLink(t.Context(), "scanner@example.com", store.SessionInfo{}); err != nil {
		t.Fatal(err)
	}
	challenge, code := magicParts(t, m.last.Text)

	// Exactly what a scanner does: fetch the URL, carrying no cookies.
	resp, err := noRedirect().Get(ts.URL + "/login/magic?c=" + url.QueryEscape(challenge) + "&k=" + url.QueryEscape(code))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the interstitial)", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "budget_session" && c.Value != "" {
			t.Fatal("a GET of the link must NOT issue a session — a mail scanner would consume it")
		}
	}

	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if !strings.Contains(page, `action="/login/magic/confirm"`) {
		t.Error("the interstitial should offer a POST to finish")
	}
	for _, want := range []string{"Confirm sign-in", "Confirmer la connexion"} {
		if !strings.Contains(page, want) {
			t.Errorf("interstitial missing %q — signed-out pages are bilingual", want)
		}
	}

	// The link is still live, because nothing consumed it.
	if err := svc.LookupMagicLink(t.Context(), challenge); err != nil {
		t.Fatal("the scan must not have consumed the link")
	}
}

// The POST behind the button is what signs in.
func TestMagicLinkPOSTSignsIn(t *testing.T) {
	ts, s, svc, m := magicFixture(t)
	verifiedUser(t, s, "confirm@example.com")

	if err := svc.RequestMagicLink(t.Context(), "confirm@example.com", store.SessionInfo{}); err != nil {
		t.Fatal(err)
	}
	challenge, code := magicParts(t, m.last.Text)

	resp := postWithHeaders(t, noRedirect(), ts.URL+"/login/magic/confirm",
		url.Values{"challenge": {challenge}, "code": {code}}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	var issued bool
	for _, c := range resp.Cookies() {
		if c.Name == "budget_session" && c.Value != "" {
			issued = true
		}
	}
	if !issued {
		t.Fatal("the POST should issue a session")
	}

	// Single use: the same link must not work twice.
	again := postWithHeaders(t, noRedirect(), ts.URL+"/login/magic/confirm",
		url.Values{"challenge": {challenge}, "code": {code}}, nil)
	defer again.Body.Close()
	if again.StatusCode == http.StatusSeeOther {
		t.Fatal("a spent link must not sign anyone in again")
	}
}

// Requesting a link must look identical for an address with no account.
func TestMagicLinkRequestIsIndistinguishable(t *testing.T) {
	ts, s, _, _ := magicFixture(t)
	verifiedUser(t, s, "known@example.com")

	client := noRedirect()
	known := postWithHeaders(t, client, ts.URL+"/login/magic", url.Values{"email": {"known@example.com"}}, nil)
	knownBody := readAll(t, known)
	unknown := postWithHeaders(t, client, ts.URL+"/login/magic", url.Values{"email": {"nobody@example.com"}}, nil)
	unknownBody := readAll(t, unknown)

	if known.StatusCode != unknown.StatusCode {
		t.Errorf("status differs: %d vs %d", known.StatusCode, unknown.StatusCode)
	}
	if knownBody != unknownBody {
		t.Error("the response must not reveal whether the address has an account")
	}
}

// The request button lives inside the sign-in form so it reuses the address
// already typed, and works with JavaScript disabled.
func TestSignInPageOffersAMagicLink(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	resp, err := noRedirect().Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if !strings.Contains(page, `formaction="/login/magic"`) {
		t.Error("the button should retarget the sign-in form, so the typed address is reused")
	}
	// The password field is `required`; without this the browser blocks a
	// submission that deliberately has no password.
	if !strings.Contains(page, "formnovalidate") {
		t.Error("the magic-link button must skip validation of the password field")
	}
	if !strings.Contains(page, "Email me a sign-in link") {
		t.Error("sign-in page should offer the link")
	}
}

// An expired or unknown link lands back on sign-in with a reason, not a blank
// interstitial that cannot succeed.
func TestExpiredMagicLinkExplainsItself(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	resp, err := noRedirect().Get(ts.URL + "/login/magic?c=nonsense&k=000000")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "has expired or has already been used") {
		t.Errorf("expected an explanation, got: %.300s", body)
	}
}
