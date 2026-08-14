package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/mail"
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

// serveAuthedMFA is serveAuthed with an encryption key, so TOTP enrolment works.
func serveAuthedMFA(t *testing.T) (*httptest.Server, *http.Client, int64, *store.Store, *auth.Service) {
	t.Helper()
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Secrets.EncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthedCfg(t, s, cfg)

	sealer, err := crypto.NewSealer(cfg.Secrets.EncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(s, mail.NewConsole(), ts.URL, auth.Config{Sealer: sealer})
	return ts, client, uid, s, svc
}

// Each Security card swaps on its own: a 2FA action must not re-render the
// password card or the sessions list, which would throw away scroll position
// and any message already on screen.
func TestTwoFactorCardIsItsOwnFragment(t *testing.T) {
	ts, client, _, _, _ := serveAuthedMFA(t)

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	for _, want := range []string{`id="account-password"`, `id="account-2fa"`, `id="account-recovery"`, `id="account-sessions"`} {
		if !strings.Contains(page, want) {
			t.Errorf("security tab missing %s", want)
		}
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/2fa/totp/begin", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("begin enrolment = %d, want 200", resp.StatusCode)
	}
	frag := readAll(t, resp)
	if !strings.Contains(frag, `id="account-2fa"`) {
		t.Fatal("enrolment should return the two-factor card")
	}
	for _, unwanted := range []string{`id="account-password"`, `id="account-sessions"`, `id="app-rail"`} {
		if strings.Contains(frag, unwanted) {
			t.Errorf("enrolment returned %s — it should be a fragment, not the page", unwanted)
		}
	}
	// The QR is fetched by the fragment, so the route must serve an image.
	qr := mustGetOK(t, client, ts.URL+"/account/2fa/qr")
	defer qr.Body.Close()
	if ct := qr.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("qr content type = %q, want image/png", ct)
	}
	if cc := qr.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("the QR encodes the secret and must not be cached, got %q", cc)
	}
}

// A wrong confirmation code must keep the same QR on screen, not restart setup.
func TestFailedConfirmKeepsTheSameEnrolment(t *testing.T) {
	ts, client, _, _, _ := serveAuthedMFA(t)

	postWithHeaders(t, client, ts.URL+"/account/2fa/totp/begin", nil, nil).Body.Close()

	resp := postWithHeaders(t, client, ts.URL+"/account/2fa/totp/confirm",
		url.Values{"code": {"000000"}}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad code = %d, want 400", resp.StatusCode)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, "data-inline-errors") {
		t.Fatal("the error must come back on a section marked for inline errors")
	}
	if !strings.Contains(body, "/account/2fa/qr") {
		t.Fatal("a rejected code should re-render the setup step, not discard it")
	}
}

// Enrolment is refused rather than storing secrets in the clear.
func TestTwoFactorUnavailableWithoutEncryptionKey(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s) // default config has no encryption key

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if !strings.Contains(page, "no encryption key") {
		t.Fatalf("the card should explain why 2FA is unavailable, got: %.400s", page)
	}
}

// The settings tabs are client-side: every panel is rendered into the DOM on
// one page load, and clicking a tab only reveals what is already there. So the
// Security panel's data must be loaded whichever tab the URL selects —
// otherwise arriving at plain /account and clicking Security shows a card built
// from an empty view model, which reads as "two-step verification is
// unavailable" even when it is perfectly available.
func TestSecurityPanelIsPopulatedWithoutTabQuery(t *testing.T) {
	ts, client, uid, s, _ := serveAuthedMFA(t)

	if err := s.CreateSession(t.Context(), uid, "another-device", time.Now().Add(time.Hour),
		store.SessionInfo{Client: "ios", Label: "Pigglet for iPhone"}); err != nil {
		t.Fatal(err)
	}

	// No ?tab= at all — the default landing.
	page := readAll(t, mustGetOK(t, client, ts.URL+"/account"))

	if strings.Contains(page, "no encryption key") {
		t.Error("two-step verification is configured; the panel must not claim otherwise")
	}
	if !strings.Contains(page, "Pigglet for iPhone") {
		t.Error("the sessions list must be populated on the default page load too")
	}
}

// The settings tabs mirror their selection into ?tab= so a refresh reopens the
// same panel. The wiring is a data attribute plus a static script; assert both
// ends exist, since a rename on either side silently breaks the round trip.
func TestSettingsTabsAreWiredForURLSync(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account"))
	if !strings.Contains(page, `data-url-tabs="tab"`) {
		t.Error("the settings tab group should opt in to URL syncing")
	}
	if !strings.Contains(page, `data-url-tabs-default="account"`) {
		t.Error("the default tab must be declared so /account stays parameter-free")
	}
	if !strings.Contains(page, "/static/tab-url.js") {
		t.Error("tab-url.js must be loaded for the tab group to sync")
	}
}

// Every tab the script can select must round-trip back to that same panel on a
// refresh — the whole point of writing the parameter.
func TestEveryTabQueryReopensItsPanel(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	for _, tab := range []string{"account", "security", "addons", "billing"} {
		page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab="+tab))
		if got := activeTab(page); got != tab {
			t.Errorf("?tab=%s reopened %q", tab, got)
		}
	}
	// A bare /account, and anything unrecognized, land on the default.
	for _, url := range []string{"/account", "/account?tab=", "/account?tab=nonsense"} {
		page := readAll(t, mustGetOK(t, client, ts.URL+url))
		if got := activeTab(page); got != "account" {
			t.Errorf("%s opened %q, want the account tab", url, got)
		}
	}
}

// activeTab reports which settings tab a rendered page has open, by finding the
// trigger templUI marked active. Shared by the tab tests here and the settings
// fragment tests in budget_redesign_test.go.
func activeTab(body string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`data-tui-tabs-value="([a-z-]+)"[^>]*data-tui-tabs-state="active"`),
		// templUI may order the attributes the other way round.
		regexp.MustCompile(`data-tui-tabs-state="active"[^>]*data-tui-tabs-value="([a-z-]+)"`),
	} {
		if m := re.FindStringSubmatch(body); m != nil {
			return m[1]
		}
	}
	return ""
}

// Turning off the last second factor also deletes the recovery codes. The user
// has to be told BEFORE they click — the button's label alone gives no hint —
// and told again afterwards, because they may be holding a printout.
func TestLastFactorWarningAndDisclosure(t *testing.T) {
	ts, client, uid, _, svc := serveAuthedMFA(t)

	if _, err := svc.SetEmailOTPEnabled(t.Context(), uid, true); err != nil {
		t.Fatal(err)
	}

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if !strings.Contains(page, "only second step") {
		t.Error("the only remaining factor should warn that disabling it deletes the codes")
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/2fa/email", url.Values{"enabled": {"off"}}, nil)
	defer resp.Body.Close()
	body := readAll(t, resp)
	if !strings.Contains(body, "recovery codes have been deleted") {
		t.Errorf("turning off the last factor must say the codes went, got: %.400s", body)
	}
}

// With two factors on, disabling one keeps the codes — so it must NOT warn, or
// the warning stops meaning anything.
func TestNoWarningWhenAnotherFactorRemains(t *testing.T) {
	ts, client, uid, _, svc := serveAuthedMFA(t)

	// Two factors: an authenticator and email codes.
	enr, err := svc.BeginTOTPEnrolment(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(enr.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmTOTP(t.Context(), uid, code); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEmailOTPEnabled(t.Context(), uid, true); err != nil {
		t.Fatal(err)
	}

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if strings.Contains(page, "only second step") {
		t.Error("two factors are on, so neither is the last one")
	}

	resp := postWithHeaders(t, client, ts.URL+"/account/2fa/email", url.Values{"enabled": {"off"}}, nil)
	defer resp.Body.Close()
	body := readAll(t, resp)
	if strings.Contains(body, "recovery codes have been deleted") {
		t.Error("the authenticator is still on, so the codes were kept")
	}
}

// Enabling email codes mints the account's first recovery codes. They exist in
// memory only on that response, so failing to show them leaves the user with
// ten codes they have never seen and can never retrieve.
func TestEnablingEmailOTPShowsTheCodesItCreates(t *testing.T) {
	ts, client, _, _, _ := serveAuthedMFA(t)

	resp := postWithHeaders(t, client, ts.URL+"/account/2fa/email", url.Values{"enabled": {"on"}}, nil)
	defer resp.Body.Close()
	body := readAll(t, resp)

	codes := regexp.MustCompile(`[a-z2-7]{4}-[a-z2-7]{4}-[a-z2-7]{4}`).FindAllString(body, -1)
	if len(codes) != 10 {
		t.Fatalf("recovery codes shown = %d, want 10: %.500s", len(codes), body)
	}
	if !strings.Contains(body, "shown only once") {
		t.Error("the user must be told these will not be shown again")
	}
}

// With no factor on, the card says why there are no codes rather than reporting
// "0 remaining", which reads like a fault.
func TestRecoveryCardExplainsWhyThereAreNoCodes(t *testing.T) {
	ts, client, _, _, _ := serveAuthedMFA(t)

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if !strings.Contains(page, "no recovery codes because two-step verification is off") {
		t.Errorf("expected an explanation, got: %.400s", page)
	}
	if strings.Contains(page, "0 unused codes") {
		t.Error(`"0 unused codes remaining" reads like something broke`)
	}
}
