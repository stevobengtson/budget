package web

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/store"
)

// The public marketing and legal pages are served at one URL per language: the
// English set at the root, the French set under /fr. The URL decides, not the
// visitor's cookie or Accept-Language — that is what makes each language
// separately indexable and separately shareable.

var publicPaths = []string{"/", "/privacy", "/terms", "/refund", "/contact"}

func TestPublicPagesExistInBothLanguages(t *testing.T) {
	ts := serveAnon(t)
	for _, p := range publicPaths {
		for _, prefix := range []string{"", "/fr"} {
			path := prefix + p
			if prefix != "" && p == "/" {
				path = prefix
			}
			resp, err := http.Get(ts.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
			}
		}
	}
}

// The address decides the language. A French browser asking for /privacy gets
// English, because that URL is the English one — otherwise the page a crawler
// indexes depends on which language it happened to ask for.
func TestPublicPageLanguageComesFromTheURLNotTheBrowser(t *testing.T) {
	ts := serveAnon(t)

	en := getLang(t, ts, "/privacy", "fr-CA,fr;q=0.9")
	if !strings.Contains(en, `<html lang="en-CA"`) {
		t.Error("/privacy must be English even for a French browser")
	}
	if !strings.Contains(en, "Privacy Policy") {
		t.Error("/privacy missing its English heading")
	}

	fr := getLang(t, ts, "/fr/privacy", "en-CA,en;q=0.9")
	if !strings.Contains(fr, `<html lang="fr-CA"`) {
		t.Error("/fr/privacy must be French even for an English browser")
	}
	if !strings.Contains(fr, "Politique de confidentialité") {
		t.Error("/fr/privacy missing its French heading")
	}
}

// Each language declares the other, so a crawler indexes both rather than
// treating one as a duplicate of the other.
func TestPublicPagesDeclareTheirAlternates(t *testing.T) {
	ts := serveAnon(t)

	for _, path := range []string{"/terms", "/fr/terms"} {
		body := getLang(t, ts, path, "en-CA")
		for _, want := range []string{
			`rel="alternate" hreflang="en-CA" href="/terms"`,
			`rel="alternate" hreflang="fr-CA" href="/fr/terms"`,
			`rel="alternate" hreflang="x-default" href="/terms"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %s", path, want)
			}
		}
	}

	// Canonical points at the page itself, not at the English one.
	if body := getLang(t, ts, "/fr/terms", "en-CA"); !strings.Contains(body, `rel="canonical" href="/fr/terms"`) {
		t.Error("/fr/terms should be its own canonical")
	}
}

// The switch is a link to the same page in the other language, so the visitor
// ends up somewhere they can share — not on the other language's home page, and
// not on the same URL with a flipped cookie.
func TestLanguageSwitchLinksToTheSamePage(t *testing.T) {
	ts := serveAnon(t)

	if body := getLang(t, ts, "/refund", "en-CA"); !strings.Contains(body, `href="/fr/refund" hreflang="fr-CA"`) {
		t.Error("English refund page should offer /fr/refund")
	}
	if body := getLang(t, ts, "/fr/refund", "en-CA"); !strings.Contains(body, `href="/refund" hreflang="en-CA"`) {
		t.Error("French refund page should offer /refund")
	}
}

// Navigation within a language must stay in that language, or a visitor is
// dropped back into English by clicking the footer.
func TestFrenchPagesLinkToFrenchPages(t *testing.T) {
	ts := serveAnon(t)
	body := getLang(t, ts, "/fr", "en-CA")
	for _, want := range []string{"/fr/privacy", "/fr/terms", "/fr/refund", "/fr/contact"} {
		if !strings.Contains(body, `href="`+want+`"`) {
			t.Errorf("French landing page missing in-language link %s", want)
		}
	}
	// And must not carry the English ones alongside them.
	for _, unwanted := range []string{`href="/privacy"`, `href="/terms"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("French landing page links out to English: %s", unwanted)
		}
	}
}

// A sentence with a link inside it stays one message in the catalog; the anchor
// is spliced in at render. Losing that would mean either fragmenting the
// sentence or dropping the link.
func TestLegalLinksAreSplicedIntoTranslatedSentences(t *testing.T) {
	ts := serveAnon(t)

	fr := getLang(t, ts, "/fr/terms", "en-CA")
	if !strings.Contains(fr, `<a href="/fr/refund"`) {
		t.Error("French terms should link to the French refund policy")
	}
	if !strings.Contains(fr, "Politique de remboursement</a>") {
		t.Error("the link text should be the French page name")
	}
	// The spliced anchor must not introduce a space before the closing period.
	if strings.Contains(fr, "</a> .") {
		t.Error("spliced link left a space before the sentence's period")
	}

	en := getLang(t, ts, "/privacy", "en-CA")
	if !strings.Contains(en, `<a href="mailto:contact@plainlysoftware.com"`) {
		t.Error("privacy policy should link the contact address")
	}
}

// The landing page redirects a signed-in visitor into the app; that must still
// hold for the French URL.
func TestFrenchLandingRedirectsSignedInVisitors(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	noRedirect := &http.Client{Jar: client.Jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/fr")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/budget" {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /fr signed in = %d -> %q (body %d bytes)",
			resp.StatusCode, resp.Header.Get("Location"), len(b))
	}
}
