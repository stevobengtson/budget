package web

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/web/views"
)

type testURLSet struct {
	URLs []struct {
		Loc        string `xml:"loc"`
		Alternates []struct {
			Rel      string `xml:"rel,attr"`
			Hreflang string `xml:"hreflang,attr"`
			Href     string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"url"`
}

func fetchSitemap(t *testing.T) (testURLSet, string) {
	t.Helper()
	ts := serveAnon(t)
	resp, err := http.Get(ts.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sitemap.xml = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type = %q, want application/xml", ct)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc testURLSet
	if err := xml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("sitemap is not well-formed XML: %v", err)
	}
	return doc, string(b)
}

// Every public page, in every language.
func TestSitemapListsEveryPublicPageInEveryLanguage(t *testing.T) {
	doc, raw := fetchSitemap(t)

	want := len(views.PublicPages) * len(i18n.Supported)
	if len(doc.URLs) != want {
		t.Fatalf("sitemap has %d <url> entries, want %d", len(doc.URLs), want)
	}

	got := map[string]bool{}
	for _, u := range doc.URLs {
		got[u.Loc] = true
	}
	for _, page := range views.PublicPages {
		for _, l := range i18n.Supported {
			// The handler builds absolute URLs from the configured base; assert on
			// the path so the test does not hard-code a host.
			path := views.PublicHref(l, page)
			found := false
			for loc := range got {
				if strings.HasSuffix(loc, path) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("sitemap missing %s (%s)\n%s", path, l, raw)
			}
		}
	}
}

// Absolute URLs are required by the sitemaps.org schema; a path-only <loc> is
// silently ignored by crawlers.
func TestSitemapURLsAreAbsolute(t *testing.T) {
	doc, _ := fetchSitemap(t)
	for _, u := range doc.URLs {
		if !strings.HasPrefix(u.Loc, "http://") && !strings.HasPrefix(u.Loc, "https://") {
			t.Errorf("<loc>%s</loc> is not absolute", u.Loc)
		}
		for _, a := range u.Alternates {
			if !strings.HasPrefix(a.Href, "http://") && !strings.HasPrefix(a.Href, "https://") {
				t.Errorf("alternate %s is not absolute", a.Href)
			}
		}
	}
}

// Each entry declares every language plus x-default, including itself — the
// reciprocity the spec asks for. A one-way declaration is ignored.
func TestSitemapEntriesDeclareAllAlternates(t *testing.T) {
	doc, _ := fetchSitemap(t)
	for _, u := range doc.URLs {
		seen := map[string]bool{}
		for _, a := range u.Alternates {
			if a.Rel != "alternate" {
				t.Errorf("%s: alternate rel = %q", u.Loc, a.Rel)
			}
			seen[a.Hreflang] = true
		}
		for _, l := range i18n.Supported {
			if !seen[string(l)] {
				t.Errorf("%s does not declare hreflang %s", u.Loc, l)
			}
		}
		if !seen["x-default"] {
			t.Errorf("%s does not declare x-default", u.Loc)
		}
	}
}

// The sitemap is only useful if what it lists actually exists — this is the
// check that catches a page renamed in the router but not in PublicPages, or
// the reverse.
func TestEverySitemapURLResolves(t *testing.T) {
	ts := serveAnon(t)
	resp, err := http.Get(ts.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var doc testURLSet
	if err := xml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, u := range doc.URLs {
		// Swap the configured base for the test server's.
		path := u.Loc
		if i := strings.Index(path[strings.Index(path, "//")+2:], "/"); i >= 0 {
			path = path[strings.Index(path, "//")+2:][i:]
		} else {
			path = "/"
		}
		r, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("sitemap lists %s, which returns %d", path, r.StatusCode)
		}
	}
}
