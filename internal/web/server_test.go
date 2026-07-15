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
	for _, path := range []string{"/budget", "/transactions", "/accounts", "/paydown"} {
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
	for _, marker := range []string{"Budget", "Transactions", "Accounts", "Paydown", `aria-current="page"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing %q in layout", marker)
		}
	}
}

func TestAccountsOverviewPartialRenders(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/accounts/overview")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)
	for _, marker := range []string{`id="accounts-overview"`, "All Accounts", "Est. Net Worth"} {
		if !strings.Contains(body, marker) {
			t.Errorf("overview fragment missing %q", marker)
		}
	}
}

// TestTransactionDeleteEmitsAccountsChanged verifies a successful delete
// triggers the sidebar's accountsChanged refresh via HX-Trigger. Deleting a
// nonexistent id (e.g. 0) errors out of DeleteTransaction before the header
// is set, so this seeds a real account + transaction first and deletes that.
func TestTransactionDeleteEmitsAccountsChanged(t *testing.T) {
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s := store.New(conn)
	srv := NewServer(s)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	acctID, err := s.CreateAccount(context.Background(), store.Account{Name: "Checking", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"account_id": {strconv.FormatInt(acctID, 10)},
		"date":       {"2026-07-14"},
		"outflow":    {"10.00"},
	}
	createResp, err := http.PostForm(ts.URL+"/transactions", form)
	if err != nil {
		t.Fatal(err)
	}
	_ = createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d, want 200", createResp.StatusCode)
	}

	rows, err := s.ListTransactions(context.Background(), store.TxFilter{AccountID: &acctID})
	if err != nil || len(rows) == 0 {
		t.Fatalf("ListTransactions() = %v, %v; want one row", rows, err)
	}
	txID := rows[0].ID

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/transactions/"+strconv.FormatInt(txID, 10), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("HX-Trigger"); got != "accountsChanged" {
		t.Errorf("HX-Trigger = %q, want accountsChanged", got)
	}
	if body := readAll(t, resp); body != "" {
		t.Errorf("delete body = %q, want empty", body)
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
