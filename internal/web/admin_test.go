package web

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
)

// newAdminServer starts a server whose seeded session user is an admin.
func newAdminServer(t *testing.T) (*httptest.Server, *http.Client, *store.Store, int64) {
	t.Helper()
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	if err := s.SetUserAdmin(context.Background(), uid, true); err != nil {
		t.Fatal(err)
	}
	return ts, client, s, uid
}

// seedSessionUser creates a second verified user (password "password1") with a
// live session, returning their id and a client carrying that session — the
// target the admin acts on.
func seedSessionUser(t *testing.T, ts *httptest.Server, s *store.Store, email string) (int64, *http.Client) {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.CreateUser(ctx, email, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	raw, err := auth.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, uid, auth.HashToken(raw), time.Now().Add(time.Hour), "test", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(ts.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: handlers.SessionCookieName, Value: raw, Path: "/"}})
	return uid, &http.Client{Jar: jar}
}

// getBody fetches url and returns the response body, failing on a non-200.
func getBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	return string(body)
}

// statusOf fetches url and returns only its status code.
func statusOf(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// postForm submits an admin action and fails unless it redirects, which is what
// every successful console mutation does.
func postForm(t *testing.T, client *http.Client, url string, form url.Values) {
	t.Helper()
	prev := client.CheckRedirect
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { client.CheckRedirect = prev }()

	resp, err := client.PostForm(url, form)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s = %d, want 303", url, resp.StatusCode)
	}
}

// postFormBody submits a form and returns the rendered body, for the cases where
// the response itself is the assertion.
func postFormBody(t *testing.T, client *http.Client, url string, form url.Values) string {
	t.Helper()
	resp, err := client.PostForm(url, form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// TestAdminRoutesRequireAdminFlag is the guard's whole point: a signed-in but
// non-admin user must not reach any console route, and must not be told the
// console exists either.
func TestAdminRoutesRequireAdminFlag(t *testing.T) {
	ts, client, _ := newTestServer(t) // seeded user is NOT an admin
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	for _, path := range []string{"/admin", "/admin/users", "/admin/chart", "/admin/users/1"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s as non-admin = %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestAdminPagesRender checks the console renders for an admin, including the
// chart partial the range toggle swaps in.
func TestAdminPagesRender(t *testing.T) {
	ts, client, _, uid := newAdminServer(t)

	for _, path := range []string{
		"/admin", "/admin/users",
		"/admin/chart?range=week", "/admin/chart?range=month", "/admin/chart?range=year",
		"/admin/users/" + strconv.FormatInt(uid, 10),
	} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		if len(body) == 0 {
			t.Errorf("GET %s returned an empty body", path)
		}
	}
}

// TestAdminChartRangeFallsBack pins that an unrecognised range is not passed
// through — it resolves to the default window rather than reaching date_trunc.
func TestAdminChartRangeFallsBack(t *testing.T) {
	ts, client, _, _ := newAdminServer(t)

	hostile := url.QueryEscape("nonsense'; DROP TABLE users; --")
	resp, err := client.Get(ts.URL + "/admin/chart?range=" + hostile)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "in the last 7 days") {
		t.Error("unknown range did not fall back to the week window")
	}
}

// TestAdminUserMenuShowsAdminEntry checks the nav entry is gated on the flag, so
// a regular user is never shown a link into the console. Granting the flag mid-
// test also exercises the claim that requireAuth re-reads it every request,
// rather than caching it for the life of the session.
//
// Both halves run against one server on purpose: openTestDB takes a global
// advisory lock on the shared test database, so standing up a second one inside
// the same test would deadlock waiting for the first to release it.
func TestAdminUserMenuShowsAdminEntry(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	if body := getBody(t, client, ts.URL+"/account"); strings.Contains(body, `href="/admin"`) {
		t.Error("non-admin sees the Admin menu entry")
	}

	if err := s.SetUserAdmin(context.Background(), uid, true); err != nil {
		t.Fatal(err)
	}
	if body := getBody(t, client, ts.URL+"/account"); !strings.Contains(body, `href="/admin"`) {
		t.Error("admin does not see the Admin menu entry")
	}
}

// TestAdminDisableUserLocksThemOut covers the enforcement path end to end: a
// disabled user's existing session stops working and their password stops
// logging them in.
func TestAdminDisableUserLocksThemOut(t *testing.T) {
	ts, client, s, adminID := newAdminServer(t)
	ctx := context.Background()

	victimID, victimClient := seedSessionUser(t, ts, s, "victim@example.com")
	if victimID == adminID {
		t.Fatal("victim and admin must differ")
	}

	// The victim's session works before the suspension.
	if code := statusOf(t, victimClient, ts.URL+"/account"); code != http.StatusOK {
		t.Fatalf("victim /account before disable = %d, want 200", code)
	}

	postForm(t, client, ts.URL+"/admin/users/"+strconv.FormatInt(victimID, 10)+"/disable", nil)

	// Their session is now rejected and bounced to /login.
	victimClient.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := victimClient.Get(ts.URL + "/account")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Errorf("disabled session = %d -> %q, want 303 -> /login",
			resp.StatusCode, resp.Header.Get("Location"))
	}

	// And a fresh login is refused with the disabled message, not a generic one.
	body := postFormBody(t, &http.Client{}, ts.URL+"/login", url.Values{
		"email":    {"victim@example.com"},
		"password": {"password1"},
	})
	if !strings.Contains(body, "disabled") {
		t.Error("login as a disabled user did not report the suspension")
	}

	// Re-enabling restores logins.
	postForm(t, client, ts.URL+"/admin/users/"+strconv.FormatInt(victimID, 10)+"/enable", nil)
	if u, err := s.GetUserByID(ctx, victimID); err != nil || u.Disabled() {
		t.Errorf("user still disabled after enable: %v %v", u.DisabledAt, err)
	}
}

// TestAdminCompGrantOpensTheGate checks a comp actually satisfies the
// subscription gate, which is the only reason the feature exists.
func TestAdminCompGrantOpensTheGate(t *testing.T) {
	ts, client, s, _ := newAdminServer(t)
	ctx := context.Background()

	targetID, _ := seedSessionUser(t, ts, s, "comped@example.com")
	target := strconv.FormatInt(targetID, 10)

	postForm(t, client, ts.URL+"/admin/users/"+target+"/comp", url.Values{"duration": {"1m"}})
	if exempt, err := s.IsBillingExempt(ctx, targetID); err != nil || !exempt {
		t.Fatalf("after 1m comp: exempt = %v err %v, want true", exempt, err)
	}

	postForm(t, client, ts.URL+"/admin/users/"+target+"/comp", url.Values{"duration": {"lifetime"}})
	if _, until, _ := s.GetComp(ctx, targetID); until != nil {
		t.Errorf("lifetime comp until = %v, want nil", until)
	}

	postForm(t, client, ts.URL+"/admin/users/"+target+"/comp/revoke", nil)
	if exempt, _ := s.IsBillingExempt(ctx, targetID); exempt {
		t.Error("comp survived revoke")
	}
}

// TestAdminCompRejectsUnknownDuration checks the duration is validated against
// the offered set rather than parsed loosely.
func TestAdminCompRejectsUnknownDuration(t *testing.T) {
	ts, client, s, _ := newAdminServer(t)
	targetID, _ := seedSessionUser(t, ts, s, "baddur@example.com")

	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.PostForm(
		ts.URL+"/admin/users/"+strconv.FormatInt(targetID, 10)+"/comp",
		url.Values{"duration": {"99y"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown duration = %d, want 400", resp.StatusCode)
	}
	if exempt, _ := s.IsBillingExempt(context.Background(), targetID); exempt {
		t.Error("rejected duration still granted a comp")
	}
}

// TestAdminDeleteUser covers the three ways deletion is gated — a live
// subscription, a mistyped confirmation, and deleting yourself — then the happy
// path.
func TestAdminDeleteUser(t *testing.T) {
	ts, client, s, adminID := newAdminServer(t)
	ctx := context.Background()
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	targetID, _ := seedSessionUser(t, ts, s, "doomed@example.com")
	target := strconv.FormatInt(targetID, 10)

	// A live subscription blocks deletion: we would stop seeing them but Stripe
	// would keep charging.
	if err := s.UpsertSubscription(ctx, store.Subscription{
		UserID: targetID, StripeSubscriptionID: "sub_del", StripeCustomerID: "cus_del",
		PriceID: "price_base", Status: "active", Currency: "usd",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := client.PostForm(ts.URL+"/admin/users/"+target+"/delete",
		url.Values{"confirm_email": {"doomed@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("delete with live subscription = %d, want 400", resp.StatusCode)
	}
	if _, err := s.GetUserByID(ctx, targetID); err != nil {
		t.Fatal("user was deleted despite a live subscription")
	}

	// Cancelled: now deletable, but the confirmation must match.
	if err := s.UpsertSubscription(ctx, store.Subscription{
		UserID: targetID, StripeSubscriptionID: "sub_del", StripeCustomerID: "cus_del",
		PriceID: "price_base", Status: "canceled", Currency: "usd",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err = client.PostForm(ts.URL+"/admin/users/"+target+"/delete",
		url.Values{"confirm_email": {"wrong@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("delete with wrong confirmation = %d, want 400", resp.StatusCode)
	}
	if _, err := s.GetUserByID(ctx, targetID); err != nil {
		t.Fatal("user was deleted despite a mismatched confirmation")
	}

	// An admin cannot delete themselves, however well they type.
	adminUser, err := s.GetUserByID(ctx, adminID)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = client.PostForm(ts.URL+"/admin/users/"+strconv.FormatInt(adminID, 10)+"/delete",
		url.Values{"confirm_email": {adminUser.Email}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("self-delete = %d, want 400", resp.StatusCode)
	}

	// Happy path.
	resp, err = client.PostForm(ts.URL+"/admin/users/"+target+"/delete",
		url.Values{"confirm_email": {"doomed@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/users" {
		t.Errorf("delete = %d -> %q, want 303 -> /admin/users",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, err := s.GetUserByID(ctx, targetID); err == nil {
		t.Error("user still present after delete")
	}
}

// TestAdminSelfDisableRefused stops an operator locking themselves out.
func TestAdminSelfDisableRefused(t *testing.T) {
	ts, client, s, adminID := newAdminServer(t)
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.PostForm(ts.URL+"/admin/users/"+strconv.FormatInt(adminID, 10)+"/disable", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("self-disable = %d, want 400", resp.StatusCode)
	}
	if u, _ := s.GetUserByID(context.Background(), adminID); u.Disabled() {
		t.Error("admin disabled themselves")
	}
}
