package web

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/store"
)

// passkeyCfg enables passkeys with a full association configuration.
func passkeyCfg(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Passkeys.RPID = "pigglet.ca"
	cfg.Passkeys.RPDisplayName = "Pigglet"
	cfg.Passkeys.Origins = []string{"https://pigglet.ca"}
	cfg.Passkeys.AppleAppIDs = []string{"ABCDE12345.ca.pigglet.budget"}
	cfg.Passkeys.AndroidPackage = "ca.pigglet.budget"
	cfg.Passkeys.AndroidFingerprints = []string{
		"AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99",
	}
	return cfg
}

// Apple fetches this document through its own CDN and fails silently on almost
// any deviation: it must be JSON, at exactly this path with no extension, and a
// 200 with no redirect.
func TestAppleAppSiteAssociationIsServedCorrectly(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	// Fetched WITHOUT a session: Apple is not signed in.
	client := noRedirect()
	resp, err := client.Get(ts.URL + "/.well-known/apple-app-site-association")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no redirect", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		WebCredentials struct {
			Apps []string `json:"apps"`
		} `json:"webcredentials"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	// webcredentials is the half that permits passkey sharing with the app.
	if len(doc.WebCredentials.Apps) != 1 || doc.WebCredentials.Apps[0] != "ABCDE12345.ca.pigglet.budget" {
		t.Fatalf("webcredentials.apps = %v", doc.WebCredentials.Apps)
	}
}

// Android needs BOTH relations. get_login_creds is the one that permits passkey
// sharing, and omitting it fails in a way that looks like a working setup.
func TestAssetLinksCarriesBothRelations(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	client := noRedirect()
	resp, err := client.Get(ts.URL + "/.well-known/assetlinks.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var doc []struct {
		Relation []string `json:"relation"`
		Target   struct {
			Namespace    string   `json:"namespace"`
			PackageName  string   `json:"package_name"`
			Fingerprints []string `json:"sha256_cert_fingerprints"`
		} `json:"target"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if len(doc) != 1 {
		t.Fatalf("entries = %d, want 1", len(doc))
	}
	rels := strings.Join(doc[0].Relation, " ")
	for _, want := range []string{"handle_all_urls", "get_login_creds"} {
		if !strings.Contains(rels, want) {
			t.Errorf("relations %v missing %s", doc[0].Relation, want)
		}
	}
	if doc[0].Target.PackageName != "ca.pigglet.budget" {
		t.Errorf("package = %q", doc[0].Target.PackageName)
	}
	// Colon-hex here, base64url in the passkey origin list — same certificate,
	// two encodings, easy to transpose.
	if len(doc[0].Target.Fingerprints) != 1 || !strings.Contains(doc[0].Target.Fingerprints[0], ":") {
		t.Errorf("fingerprints = %v, want colon-separated hex", doc[0].Target.Fingerprints)
	}
}

// These are machine-read documents at fixed paths, not indexable pages. Putting
// them in views.PublicPages would mirror them per language and list them in the
// sitemap, both of which are wrong.
func TestWellKnownDocumentsAreNotInTheSitemap(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	resp, err := noRedirect().Get(ts.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	for _, unwanted := range []string{"well-known", "assetlinks", "apple-app-site"} {
		if strings.Contains(string(body), unwanted) {
			t.Errorf("sitemap should not list %q", unwanted)
		}
	}
}

// With no RP domain configured the feature is off, and the card says so rather
// than offering a button that cannot work.
func TestPasskeyCardSaysWhenUnavailable(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s) // default config has no passkey RP ID

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account?tab=security"))
	if !strings.Contains(page, "no relying-party domain") {
		t.Errorf("expected an explanation, got: %.400s", page)
	}
}

// The sign-in page must keep working with JavaScript disabled, so the passkey
// button ships hidden and passkey.js reveals it only where WebAuthn exists.
func TestPasskeyLoginButtonIsHiddenByDefault(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	resp, err := noRedirect().Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if !strings.Contains(page, "data-passkey-login") {
		t.Fatal("the login page should carry the passkey button")
	}
	// "hidden" on the element itself: with JS off it never appears, and the
	// ordinary password form is still fully functional.
	if !strings.Contains(page, "data-passkey-login hidden") {
		t.Error("the passkey button must ship hidden and be revealed by script")
	}
	if !strings.Contains(page, "/static/passkey.js") {
		t.Error("passkey.js must be loaded on the sign-in page")
	}
	// The password form is still there and still posts.
	if !strings.Contains(page, `action="/login"`) {
		t.Error("the plain password form must remain")
	}
}

// The card is part of the Security tab and swaps independently, like the others.
func TestPasskeyCardIsOnTheSecurityTab(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	page := readAll(t, mustGetOK(t, client, ts.URL+"/account"))
	if !strings.Contains(page, `id="account-passkeys"`) {
		t.Error("the security tab should contain the passkey card")
	}
	if !strings.Contains(page, "data-passkey-register") {
		t.Error("the card should offer to add a passkey")
	}

	frag := readAll(t, mustGetOK(t, client, ts.URL+"/account/passkeys"))
	if !strings.Contains(frag, `id="account-passkeys"`) {
		t.Error("the fragment endpoint should return just the card")
	}
	for _, unwanted := range []string{`id="account-password"`, `id="app-rail"`} {
		if strings.Contains(frag, unwanted) {
			t.Errorf("fragment leaked %s", unwanted)
		}
	}
}

// A ceremony cannot be finished without the cookie the begin call set.
func TestPasskeyFinishWithoutCeremonyIsRejected(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	resp, err := noRedirect().Post(ts.URL+"/webauthn/login/finish", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// Beginning a sign-in sets the ceremony cookie, and it must not be readable by
// script or sent on a cross-site request.
func TestPasskeyCeremonyCookieIsHardened(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthedCfg(t, s, passkeyCfg(t))

	resp, err := noRedirect().Post(ts.URL+"/webauthn/login/begin", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var found bool
	for _, c := range resp.Cookies() {
		if c.Name != "budget_webauthn" {
			continue
		}
		found = true
		if !c.HttpOnly {
			t.Error("the ceremony cookie must be HttpOnly")
		}
		if c.SameSite != http.SameSiteStrictMode {
			t.Error("a passkey ceremony is never started by another site; SameSite must be Strict")
		}
	}
	if !found {
		t.Fatal("begin should set the ceremony cookie")
	}

	// The options are handed to the browser verbatim; Android rejects them
	// without an rpId or with a zero timeout.
	body, _ := io.ReadAll(resp.Body)
	var opts map[string]any
	if err := json.Unmarshal(body, &opts); err != nil {
		t.Fatalf("options are not JSON: %v", err)
	}
	pk, ok := opts["publicKey"].(map[string]any)
	if !ok {
		t.Fatalf("no publicKey in options: %s", body)
	}
	if pk["rpId"] == nil {
		t.Error("options must carry rpId")
	}
	if timeout, _ := pk["timeout"].(float64); timeout <= 0 {
		t.Error("options must carry a positive timeout")
	}
}

// A stale session must not fail silently on the passkey path.
//
// The settings cards handle step-up through the HTMX inline-error contract, but
// passkey enrolment is driven by fetch() from passkey.js, which cannot render
// an HTML card. Answering it with the card produced a 403 the script could not
// parse and swallowed — a console error and nothing else on screen.
func TestPasskeyRegisterBeginAnswersStaleSessionAsJSON(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthedCfg(t, s, passkeyCfg(t))

	// Age the session past the re-auth window.
	if _, err := s.DB().Exec(
		`UPDATE sessions SET created_at = CURRENT_TIMESTAMP - interval '2 hours', reauth_at = NULL
		 WHERE user_id = $1`, uid); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/account/passkeys/register/begin", nil)
	if err != nil {
		t.Fatal(err)
	}
	// What passkey.js asks for.
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content type = %q — a fetch() caller cannot use an HTML card", ct)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, raw)
	}
	if body.Error.Code != "reauth_required" {
		t.Fatalf("error code = %q, want reauth_required so the script can act on it", body.Error.Code)
	}
}

// The prompt has to be fetchable, or a script-driven action blocked by step-up
// has no way to offer it.
func TestReauthPromptIsFetchable(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthedCfg(t, s, passkeyCfg(t))

	// Even with a stale session: needing step-up to be OFFERED step-up would be
	// a dead end.
	if _, err := s.DB().Exec(
		`UPDATE sessions SET created_at = CURRENT_TIMESTAMP - interval '2 hours', reauth_at = NULL
		 WHERE user_id = $1`, uid); err != nil {
		t.Fatal(err)
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/account/reauth"))
	if !strings.Contains(body, `id="account-reauth"`) {
		t.Fatalf("expected the re-auth card, got: %.300s", body)
	}
	if !strings.Contains(body, "data-inline-errors") {
		t.Error("the card must carry the inline-error marker so its own errors swap in place")
	}
	if !strings.Contains(body, `hx-post="/account/reauth"`) {
		t.Error("the card must post back to the step-up endpoint")
	}
}
