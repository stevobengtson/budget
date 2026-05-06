package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
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
	for _, path := range []string{"/budget", "/transactions", "/accounts", "/categories", "/paydown", "/reports/spending", "/reports/cashflow"} {
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
	for _, marker := range []string{"Budget", "Transactions", "Accounts", "Categories", "Paydown", "Reports", "tab--active"} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing %q in layout", marker)
		}
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
