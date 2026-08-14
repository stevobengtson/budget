package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

// The signed-out auth pages show every supported language at once. A visitor
// there has no stored preference — only Accept-Language, which is a guess — and
// someone who cannot read a wrong guess cannot get far enough to fix it.

func serveAnon(t *testing.T) *httptest.Server {
	t.Helper()
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(store.New(openTestDB(t)), cfg)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// getLang fetches a page as a browser asking for acceptLang.
func getLang(t *testing.T, ts *httptest.Server, path, acceptLang string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Language", acceptLang)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Both languages appear on every signed-out auth page, whichever one the
// browser asked for.
func TestAuthPagesShowBothLanguages(t *testing.T) {
	ts := serveAnon(t)

	pages := map[string][]string{
		"/login":  {"Sign In", "Se connecter", "Email", "Courriel", "Password", "Mot de passe"},
		"/signup": {"Sign Up", "S&#39;inscrire", "Email", "Courriel"},
		"/forgot": {"Send reset link", "Envoyer le lien de réinitialisation"},
	}
	for _, accept := range []string{"en-CA", "fr-CA"} {
		for path, wants := range pages {
			body := getLang(t, ts, path, accept)
			for _, w := range wants {
				if !strings.Contains(body, w) {
					t.Errorf("%s (Accept-Language: %s) missing %q", path, accept, w)
				}
			}
		}
	}
}

// The viewer's own language leads, so the pair reads naturally to whoever the
// browser did identify — and the other language is still right there.
func TestBilingualOrderFollowsTheViewersLocale(t *testing.T) {
	ts := serveAnon(t)

	if body := getLang(t, ts, "/login", "en-CA"); !strings.Contains(body, "Email / Courriel") {
		t.Error("an English browser should see \"Email / Courriel\"")
	}
	if body := getLang(t, ts, "/login", "fr-CA"); !strings.Contains(body, "Courriel / Email") {
		t.Error("a French browser should see \"Courriel / Email\"")
	}
}

// Validation errors are the part a stuck visitor most needs to read, so they
// are bilingual too — stacked, not slash-joined, because a sentence joined with
// a slash is unreadable in both languages at once.
func TestAuthErrorsAreBilingual(t *testing.T) {
	ts := serveAnon(t)

	resp, err := http.PostForm(ts.URL+"/login", url.Values{
		"email": {"nobody@example.com"}, "password": {"wrong"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	body := string(b)

	for _, want := range []string{
		"Invalid email or password.",
		"Adresse courriel ou mot de passe invalide.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login error page missing %q", want)
		}
	}
	// Slash-joining a sentence is what this must not do.
	if strings.Contains(body, "Invalid email or password. / ") {
		t.Error("sentences must stack, not slash-join")
	}
}

// A word spelled the same in both languages must not render as "X / X".
func TestIdenticalRenderingsCollapse(t *testing.T) {
	ts := serveAnon(t)
	body := getLang(t, ts, "/login", "en-CA")
	if strings.Contains(body, "Email / Email") || strings.Contains(body, "Courriel / Courriel") {
		t.Error("identical renderings should collapse to one")
	}
}

// The signed-in paywall reuses the same card shell but must stay single
// language: that user is authenticated and has a stored preference.
func TestBillingPaywallIsNotBilingual(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	resp, err := client.Get(ts.URL + "/billing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if body := string(b); strings.Contains(body, "Subscribe / S&#39;abonner") {
		t.Error("the billing page should render in the user's language only")
	}
}

// The challenge page is signed-out too: the visitor has proved a password but
// still has no stored language preference, so it must render both languages
// like every other pre-auth screen.
func TestChallengePageIsBilingual(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, _, _ := serveAuthed(t, s)

	sealer, err := crypto.NewSealer("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(s, mail.NewConsole(), ts.URL, auth.Config{Sealer: sealer})

	ctx := t.Context()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.CreateUser(ctx, "twofa@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEmailOTPEnabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}

	for _, accept := range []string{"en-CA", "fr-CA"} {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/login",
			strings.NewReader("email=twofa@example.com&password=password1"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept-Language", accept)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		page := string(body)
		for _, want := range []string{"Two-step verification", "Vérification en deux étapes", "Verify", "Vérifier"} {
			if !strings.Contains(page, want) {
				t.Errorf("challenge page (Accept-Language: %s) missing %q", accept, want)
			}
		}
		// The one-time-code affordance is what makes iOS offer the emailed code.
		if !strings.Contains(page, `autocomplete="one-time-code"`) {
			t.Error("the code field must carry autocomplete=one-time-code")
		}
	}
}
