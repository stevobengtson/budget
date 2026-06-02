package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/settings"
	"github.com/sbengtson/budget/internal/core/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s := store.New(conn)
	srv := NewServer(s)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

type testServer struct {
	ts    *httptest.Server
	store *store.Store
}

func newTestServerWithStore(t *testing.T) *testServer {
	t.Helper()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s := store.New(conn)
	srv := NewServer(s)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &testServer{ts: ts, store: s}
}

func TestTransactionsNewUsesConfiguredDefault(t *testing.T) {
	srv := newTestServerWithStore(t)
	ctx := context.Background()
	_, _ = srv.store.CreateAccount(ctx, store.Account{Name: "Alpha", Type: store.TypeChecking})
	id2, _ := srv.store.CreateAccount(ctx, store.Account{Name: "Beta", Type: store.TypeChecking})
	if err := srv.store.SetSetting(ctx, settings.DefaultAccountKey, strconv.FormatInt(id2, 10)); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.ts.URL + "/transactions/new")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readAll(t, resp)

	wantSelected := `value="` + strconv.FormatInt(id2, 10) + `" selected`
	if !strings.Contains(body, wantSelected) {
		t.Errorf("expected configured default (id %d) to be selected; want substring %q in body", id2, wantSelected)
	}
}

func TestRedirectRoot(t *testing.T) {
	ts := newTestServer(t)
	c := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/budget" {
		t.Errorf("location = %q, want /budget", loc)
	}
}

func TestPagesRender200(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{"/budget", "/transactions", "/accounts", "/categories", "/paydown", "/settings"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestBudgetTabAppearsInLayout(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := http.Get(ts.URL + "/budget")
	body := readAll(t, resp)
	for _, marker := range []string{"Budget", "Transactions", "Accounts", "Categories", "Paydown", "Settings", `aria-current="page"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing %q in layout", marker)
		}
	}
}

func TestSettingsPageRenders(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)
	for _, marker := range []string{"Default account", "Default category", `name="default_account_id"`, `name="default_category_id"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing %q in settings page", marker)
		}
	}
}

func TestSettingsPostRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	form := url.Values{}
	form.Set("default_account_id", "")
	form.Set("default_category_id", "")
	resp, err := http.PostForm(ts.URL+"/settings", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf)
}
