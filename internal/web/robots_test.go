package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/web/views"
)

// fetchRobots takes an already-running server rather than starting its own:
// openTestDB takes the shared advisory lock per call and only releases it in
// t.Cleanup, so a second serveAnon inside one test blocks on the first forever.
func fetchRobots(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, err := http.Get(ts.URL + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /robots.txt = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// disallowed reports the rule blocking path, by robots.txt's prefix-match
// semantics, or "" when nothing blocks it.
func disallowed(body, path string) string {
	for _, line := range strings.Split(body, "\n") {
		rule, ok := strings.CutPrefix(strings.TrimSpace(line), "Disallow:")
		if !ok {
			continue
		}
		rule = strings.TrimSpace(rule)
		if rule != "" && strings.HasPrefix(path, rule) {
			return rule
		}
	}
	return ""
}

// The whole point of the file is that the pages worth indexing stay crawlable.
// A rule added later that happens to prefix-match a public page — or a stray
// "Disallow: /" — fails here rather than quietly deindexing the site.
func TestRobotsAllowsEveryPublicPage(t *testing.T) {
	body := fetchRobots(t, serveAnon(t))
	for _, page := range views.PublicPages {
		for _, l := range i18n.Supported {
			path := views.PublicHref(l, page)
			if rule := disallowed(body, path); rule != "" {
				t.Errorf("public page %s (%s) is blocked by %q", path, l, "Disallow: "+rule)
			}
		}
	}
}

// The signed-in app and the token-carrying links are the paths that must stay
// out; /reset and /verify especially, since their URLs contain a single-use
// token.
func TestRobotsBlocksAppAndTokenPaths(t *testing.T) {
	body := fetchRobots(t, serveAnon(t))
	for _, path := range []string{
		"/budget", "/transactions", "/paydown", "/accounts/new",
		"/account/billing", "/admin/users", "/billing", "/welcome",
		"/api/v1/health", "/webhooks/stripe",
		"/verify?token=abc", "/reset?token=abc",
	} {
		if rule := disallowed(body, path); rule == "" {
			t.Errorf("%s is crawlable", path)
		}
	}
}

// A Sitemap line with a relative URL is ignored, and one pointing at a path that
// does not exist is worse than none.
func TestRobotsPointsAtTheSitemap(t *testing.T) {
	ts := serveAnon(t)
	body := fetchRobots(t, ts)

	var loc string
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Sitemap:"); ok {
			loc = strings.TrimSpace(v)
		}
	}
	if loc == "" {
		t.Fatalf("no Sitemap directive:\n%s", body)
	}
	if !strings.HasPrefix(loc, "http://") && !strings.HasPrefix(loc, "https://") {
		t.Errorf("Sitemap %q is not absolute", loc)
	}
	if !strings.HasSuffix(loc, "/sitemap.xml") {
		t.Errorf("Sitemap %q does not point at /sitemap.xml", loc)
	}

	// The advertised path has to be served, whatever origin it names.
	resp, err := http.Get(ts.URL + "/sitemap.xml")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the advertised sitemap returns %d", resp.StatusCode)
	}
}
