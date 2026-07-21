package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
)

// testDBLockKey MUST match the store/db packages' advisory-lock key so every
// DB-backed test across the parallel package binaries serializes on the shared
// budget_test database.
const testDBLockKey = 918273645

func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "sessions", "users",
}

// openTestDB opens the shared Postgres test database under a global advisory
// lock, migrates, truncates, and re-seeds the global Income group/category
// (matching migration 00005). See the store package's openTestDB for details.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	lockConn, _, err := db.Open(testDSN(), false)
	if err != nil {
		t.Fatalf("open lock conn: %v", err)
	}
	lockConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = lockConn.Close() })
	if _, err := lockConn.Exec("SELECT pg_advisory_lock($1)", testDBLockKey); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}

	conn, dialect, err := db.Open(testDSN(), false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.MigrateUp(conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := conn.Exec("TRUNCATE TABLE " + strings.Join(testTables, ", ") + " RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var gid int64
	if err := conn.QueryRow(
		`INSERT INTO category_groups(name, sort_order) VALUES ('Income', -100) RETURNING id`).Scan(&gid); err != nil {
		t.Fatalf("seed income group: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO categories(group_id, name, is_income, sort_order) VALUES ($1, 'Income', TRUE, 0)`, gid); err != nil {
		t.Fatalf("seed income category: %v", err)
	}
	return conn
}

func newTestServer(t *testing.T) (*httptest.Server, *http.Client, int64) {
	t.Helper()
	return serveAuthed(t, store.New(openTestDB(t)))
}

// serveAuthed starts the server for store s and returns it plus an http.Client
// whose cookie jar carries a valid session for a seeded, verified user, and the
// seeded user's id. All app routes now sit behind requireAuth, so tests must
// present a session cookie. The seeded user claims any pre-auth (NULL-owner)
// rows so the migration-seeded Income category is owned by (and visible to) it.
func serveAuthed(t *testing.T, s *store.Store) (*httptest.Server, *http.Client, int64) {
	t.Helper()
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(s, cfg)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx := context.Background()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.CreateUser(ctx, "test@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimOrphanData(ctx, uid); err != nil {
		t.Fatal(err)
	}
	raw, err := auth.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, uid, auth.HashToken(raw), time.Now().Add(time.Hour), "test", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(ts.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: handlers.SessionCookieName, Value: raw, Path: "/"}})
	return ts, &http.Client{Jar: jar}, uid
}

func TestRedirectRoot(t *testing.T) {
	ts, client, _ := newTestServer(t)
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(ts.URL + "/")
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
	ts, client, _ := newTestServer(t)
	for _, path := range []string{"/budget", "/transactions?account=1", "/paydown"} {
		resp, err := client.Get(ts.URL + path)
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
	ts, client, _ := newTestServer(t)
	resp, _ := client.Get(ts.URL + "/budget")
	body := readAll(t, resp)
	for _, marker := range []string{"Budget", "Paydown", `data-tui-sidebar-active="true"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing %q in layout", marker)
		}
	}
}

func TestAccountsOverviewPartialRenders(t *testing.T) {
	ts, client, _ := newTestServer(t)
	resp, err := client.Get(ts.URL + "/accounts/overview")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)
	for _, marker := range []string{`id="accounts-overview"`, "Est. Net Worth"} {
		if !strings.Contains(body, marker) {
			t.Errorf("overview fragment missing %q", marker)
		}
	}
}

// TestUnauthenticatedRedirectsToLogin verifies an anonymous client (no session
// cookie) is bounced to /login from a protected app route.
func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	ts, _, _ := newTestServer(t)
	anon := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := anon.Get(ts.URL + "/budget")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("anonymous /budget = %d %q, want 303 /login", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// TestTransactionDeleteEmitsAccountsChanged verifies a successful delete
// triggers the sidebar's accountsChanged refresh via HX-Trigger. Deleting a
// nonexistent id (e.g. 0) errors out of DeleteTransaction before the header
// is set, so this seeds a real account + transaction first and deletes that.
func TestTransactionDeleteEmitsAccountsChanged(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	acctID, err := s.CreateAccount(context.Background(), uid, store.Account{Name: "Checking", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"account_id": {strconv.FormatInt(acctID, 10)},
		"date":       {"2026-07-14"},
		"outflow":    {"10.00"},
	}
	createResp, err := client.PostForm(ts.URL+"/transactions", form)
	if err != nil {
		t.Fatal(err)
	}
	_ = createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d, want 200", createResp.StatusCode)
	}

	rows, err := s.ListTransactions(context.Background(), uid, store.TxFilter{AccountID: &acctID})
	if err != nil || len(rows) == 0 {
		t.Fatalf("ListTransactions() = %v, %v; want one row", rows, err)
	}
	txID := rows[0].ID

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/transactions/"+strconv.FormatInt(txID, 10), nil)
	resp, err := client.Do(req)
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

func TestBudgetSetRolloverMode(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	ctx := context.Background()
	gid, _ := s.CreateGroup(ctx, uid, "G", 0)
	cat, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Fun"})

	form := url.Values{"mode": {store.RolloverCarry}}
	resp, err := client.PostForm(ts.URL+"/budget/category/"+strconv.FormatInt(cat, 10)+"/rollover?month=2026-07", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cats, _ := s.ListCategories(ctx, uid, false)
	got := findCatMode(cats, cat)
	if got != store.RolloverCarry {
		t.Errorf("rollover_mode = %q, want %q", got, store.RolloverCarry)
	}

	// Invalid mode is rejected.
	bad, _ := client.PostForm(ts.URL+"/budget/category/"+strconv.FormatInt(cat, 10)+"/rollover?month=2026-07",
		url.Values{"mode": {"bogus"}})
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid mode status = %d, want 400", bad.StatusCode)
	}
}

// findCatMode returns the rollover mode of the category with the given ID, or
// "" if not found. ListCategories includes the seeded system Income category,
// so the created category is located by ID rather than slice index.
func findCatMode(cats []store.Category, id int64) string {
	for _, c := range cats {
		if c.ID == id {
			return c.RolloverMode
		}
	}
	return ""
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
