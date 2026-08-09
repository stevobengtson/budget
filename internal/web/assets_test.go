package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sbengtson/budget/internal/core/store"
)

// TestAssetHashesTrackContent is the property the whole scheme rests on: the
// hash must change when the bytes change and hold steady when they do not.
// Without the first the cache is never busted; without the second every deploy
// re-downloads everything.
func TestAssetHashesTrackContent(t *testing.T) {
	before := assetHashes(fstest.MapFS{
		"app.css":      {Data: []byte("body{color:red}")},
		"quick-add.js": {Data: []byte("console.log(1)")},
	})
	after := assetHashes(fstest.MapFS{
		"app.css":      {Data: []byte("body{color:blue}")}, // changed
		"quick-add.js": {Data: []byte("console.log(1)")},   // unchanged
	})

	if before["/static/app.css"] == "" {
		t.Fatal("no hash produced for app.css")
	}
	if before["/static/app.css"] == after["/static/app.css"] {
		t.Error("app.css changed but its hash did not — the cache would never bust")
	}
	if before["/static/quick-add.js"] != after["/static/quick-add.js"] {
		t.Error("quick-add.js is unchanged but its hash moved — needless re-downloads")
	}
	// Distinct files must not collide, or one would mask the other's changes.
	if before["/static/app.css"] == before["/static/quick-add.js"] {
		t.Error("different files produced the same hash")
	}
}

// TestStaticAssetsAreVersionedAndCached checks the rendered URLs and the cache
// headers together: a versioned URL can never go stale so it is cached hard,
// while a bare one might be anything and must revalidate.
func TestStaticAssetsAreVersionedAndCached(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget"))
	versioned := regexp.MustCompile(`/static/app\.css\?v=[0-9a-f]{8}`).FindString(body)
	if versioned == "" {
		t.Fatal("app.css is not rendered with a version")
	}
	if !regexp.MustCompile(`/static/quick-add\.js\?v=[0-9a-f]{8}`).MatchString(body) {
		t.Error("scripts should be versioned too, not just the stylesheet")
	}

	fetch := func(path string) (*http.Response, string) {
		t.Helper()
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp, readAll(t, resp)
	}

	vResp, vBody := fetch(versioned)
	if vResp.StatusCode != http.StatusOK || len(vBody) == 0 {
		t.Fatalf("versioned asset returned %d, %d bytes", vResp.StatusCode, len(vBody))
	}
	if cc := vResp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("versioned asset Cache-Control = %q, want immutable", cc)
	}

	bResp, bBody := fetch("/static/app.css")
	if bBody != vBody {
		t.Error("versioned and bare URLs must serve identical bytes")
	}
	if cc := bResp.Header.Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("bare asset Cache-Control = %q, must not be immutable", cc)
	}

	// The rewritten route must not have broken ordinary serving.
	if r, _ := fetch("/static/htmx.min.js"); r.StatusCode != http.StatusOK {
		t.Errorf("htmx.min.js returned %d", r.StatusCode)
	}
	if r, _ := fetch("/static/does-not-exist.css"); r.StatusCode != http.StatusNotFound {
		t.Errorf("missing asset returned %d, want 404", r.StatusCode)
	}
}
