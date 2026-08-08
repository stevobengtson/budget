package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
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
	return serveAuthedCfg(t, s, cfg)
}

// serveAuthedCfg is serveAuthed with a caller-supplied config, used by the
// billing tests to enable Stripe (base price + webhook secret) so the
// subscription gate is live.
func serveAuthedCfg(t *testing.T, s *store.Store, cfg config.Config) (*httptest.Server, *http.Client, int64) {
	t.Helper()
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
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	// /paydown is gated behind its add-on; enable it so the page actually renders.
	if err := s.SetAddOnEnabled(context.Background(), uid, "paydown", true); err != nil {
		t.Fatal(err)
	}
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
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	// Paydown is an opt-in add-on; enable it so its nav item renders.
	if err := s.SetAddOnEnabled(context.Background(), uid, "paydown", true); err != nil {
		t.Fatal(err)
	}
	resp, _ := client.Get(ts.URL + "/budget")
	body := readAll(t, resp)
	// The primary nav is Budget and Paydown. Transactions is reached through the
	// account list ("All Accounts" heads it), which loads as a separate htmx
	// fragment, so it is not in this HTML — see
	// TestAccountsOverviewHighlightsCurrentAccount. aria-current marks the active
	// entry now that the templUI sidebar (and data-tui-sidebar-active) is gone.
	for _, marker := range []string{"Budget", "Paydown", `aria-current="page"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing %q in layout", marker)
		}
	}
}

// TestPaydownAddOnGating verifies the Paydown add-on gates both its nav item and
// its route: hidden and redirected when disabled, shown and served when enabled.
func TestPaydownAddOnGating(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	// Disabled by default: no nav item, and /paydown redirects to /budget.
	resp, _ := client.Get(ts.URL + "/budget")
	if body := readAll(t, resp); strings.Contains(body, "Paydown") {
		t.Error("Paydown nav item shown while add-on disabled")
	}
	resp, _ = client.Get(ts.URL + "/paydown")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/budget" {
		t.Fatalf("disabled /paydown = %d %q, want 303 /budget", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Enabled: nav item appears and /paydown serves 200.
	if err := s.SetAddOnEnabled(context.Background(), uid, "paydown", true); err != nil {
		t.Fatal(err)
	}
	resp, _ = client.Get(ts.URL + "/budget")
	if body := readAll(t, resp); !strings.Contains(body, "Paydown") {
		t.Error("Paydown nav item missing while add-on enabled")
	}
	resp, _ = client.Get(ts.URL + "/paydown")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enabled /paydown = %d, want 200", resp.StatusCode)
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
	// The seeded user has no accounts, so the fragment shows the empty state (no
	// net-worth line until an account exists).
	// The fragment is marked by an attribute, not an id: it mounts twice (desktop
	// sidebar and the Budget page's mobile block), and duplicate ids are invalid.
	for _, marker := range []string{"data-accounts-overview", "No accounts yet"} {
		if !strings.Contains(body, marker) {
			t.Errorf("overview fragment missing %q", marker)
		}
	}
}

// TestAccountsOverviewHighlightsCurrentAccount covers the sidebar highlight
// wiring. The fragment is fetched separately from the page, so the selected
// account can only come from the HX-Current-URL header HTMX sends.
func TestAccountsOverviewHighlightsCurrentAccount(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	chequing, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Savings", Type: store.TypeSavings}); err != nil {
		t.Fatal(err)
	}

	get := func(currentURL string) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/accounts/overview", nil)
		if currentURL != "" {
			req.Header.Set("HX-Current-URL", currentURL)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		return readAll(t, resp)
	}

	onAccount := get(ts.URL + "/transactions?account=" + strconv.FormatInt(chequing, 10) + "&month=2026-07")
	if got := strings.Count(onAccount, `aria-current="page"`); got != 1 {
		t.Errorf("aria-current count = %d, want 1 when an account is in the URL", got)
	}

	// The budget page carries no account, so nothing should be highlighted.
	if body := get(ts.URL + "/budget"); strings.Contains(body, `aria-current="page"`) {
		t.Error("no account should be highlighted on /budget")
	}
	// An account-scoped view highlights that account, not the All Accounts head.
	if got := currentRowLabel(t, onAccount); got != "Chequing" {
		t.Errorf("highlighted row = %q, want Chequing", got)
	}
	// A category-only transactions view IS the all-accounts view, so the head row
	// is the active one — no individual account is.
	catOnly := get(ts.URL + "/transactions?month=2026-07&category=3")
	if got := strings.Count(catOnly, `aria-current="page"`); got != 1 {
		t.Errorf("aria-current count = %d, want 1 (All Accounts) on a category-only view", got)
	}
	if got := currentRowLabel(t, catOnly); got != "All Accounts" {
		t.Errorf("highlighted row = %q, want All Accounts", got)
	}
	// Non-HTMX fetches (no header) must not error or guess.
	if body := get(""); strings.Contains(body, `aria-current="page"`) {
		t.Error("no header should mean no highlight")
	}

	// The month carries into the account links, so a past-month view keeps its
	// month when switching accounts. /budget uses the same param name.
	// (url.Values.Encode sorts params, and templ escapes & as &amp;.)
	fromBudget := get(ts.URL + "/budget?month=2026-05")
	if want := "account=" + strconv.FormatInt(chequing, 10) + "&amp;month=2026-05"; !strings.Contains(fromBudget, want) {
		t.Errorf("account links missing carried month %q", want)
	}
	// Without a month anywhere, links omit it and the page falls back to today.
	plain := get(ts.URL + "/budget")
	if strings.Contains(plain, "month=") {
		t.Error("account links should omit month when the current view has none")
	}

	// The category filter carries too, so switching accounts from a filtered
	// transactions view changes only the account.
	filtered := get(ts.URL + "/transactions?month=2026-05&account=" +
		strconv.FormatInt(chequing, 10) + "&category=42")
	if want := "&amp;category=42&amp;month=2026-05"; !strings.Contains(filtered, want) {
		t.Errorf("account links missing carried category %q", want)
	}
	// Every account link points at its own account, not the one being viewed.
	if strings.Count(filtered, "category=42") < 2 {
		t.Error("category should be carried onto every account link, not just the active one")
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
		"tx_type":    {"expense"},
		"account_id": {strconv.FormatInt(acctID, 10)},
		"date":       {"2026-07-14"},
		"amount":     {"10.00"},
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

func TestTransactionCreateByType(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	a, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: store.TypeChecking})
	b, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Savings", Type: store.TypeChecking})

	post := func(form url.Values) {
		t.Helper()
		resp, err := client.PostForm(ts.URL+"/transactions", form)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create status = %d, want 200", resp.StatusCode)
		}
	}

	// Expense: single amount routes to outflow.
	post(url.Values{
		"tx_type": {"expense"}, "account_id": {strconv.FormatInt(a, 10)},
		"date": {"2026-07-10"}, "amount": {"25.00"},
	})
	// Income: single amount routes to inflow.
	post(url.Values{
		"tx_type": {"income"}, "account_id": {strconv.FormatInt(a, 10)},
		"date": {"2026-07-11"}, "amount": {"40.00"},
	})
	// Transfer: outflow from a into b.
	post(url.Values{
		"tx_type": {"transfer"}, "account_id": {strconv.FormatInt(a, 10)},
		"transfer_to": {strconv.FormatInt(b, 10)}, "date": {"2026-07-12"}, "amount": {"15.00"},
	})

	aRows, _ := s.ListTransactions(ctx, uid, store.TxFilter{AccountID: &a})
	var exp, inc, xfer *store.Transaction
	for i := range aRows {
		r := &aRows[i]
		switch {
		case r.TransferAccountID != nil:
			xfer = r
		case r.InflowCents == 4000 && r.OutflowCents == 0:
			inc = r
		case r.OutflowCents == 2500 && r.InflowCents == 0:
			exp = r
		}
	}
	if exp == nil {
		t.Error("expense: want a row with outflow 2500, inflow 0")
	}
	if inc == nil {
		t.Error("income: want a row with inflow 4000, outflow 0")
	}
	if xfer == nil || xfer.OutflowCents != 1500 || xfer.TransferAccountID == nil || *xfer.TransferAccountID != b {
		t.Errorf("transfer: want a→b outflow leg of 1500, got %+v", xfer)
	}

	// The transfer's other leg posts an inflow into b.
	bRows, _ := s.ListTransactions(ctx, uid, store.TxFilter{AccountID: &b})
	if len(bRows) != 1 || bRows[0].InflowCents != 1500 || bRows[0].TransferAccountID == nil || *bRows[0].TransferAccountID != a {
		t.Errorf("transfer dest leg: want inflow 1500 from a, got %+v", bRows)
	}
}

// TestTransactionsCategoryFilter covers the budget page's "View Transactions"
// action. The view is scoped to one category across every account (no account
// filter), so it matches the budget's Spent figure, and names the category in
// its heading.
func TestTransactionsCategoryFilter(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	chequing, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	visa, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Visa", Type: store.TypeChecking})
	gid, _ := s.CreateGroup(ctx, uid, "Everyday", 0)
	groceries, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Groceries"})
	dining, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Dining"})

	// Same category, two different accounts — both belong in the view.
	mkTx(t, s, uid, chequing, groceries, "2026-07-04", "Loblaws")
	mkTx(t, s, uid, visa, groceries, "2026-07-09", "Costco")
	// Another category, and the same category in another month — both excluded.
	mkTx(t, s, uid, chequing, dining, "2026-07-05", "CornerCafe")
	mkTx(t, s, uid, chequing, groceries, "2026-06-04", "OldMart")

	resp, err := client.Get(ts.URL + "/transactions?category=" + strconv.FormatInt(groceries, 10) + "&month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)

	for _, want := range []string{"Loblaws", "Costco"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; want every account's transactions in the category", want)
		}
	}
	for _, notWant := range []string{"CornerCafe", "OldMart"} {
		if strings.Contains(body, notWant) {
			t.Errorf("body contains %q; want it filtered out by category+month", notWant)
		}
	}
	// The filter bar must reflect the active filter, not just the row set.
	if want := selectboxSelected(groceries); !strings.Contains(body, want) {
		t.Errorf("filter bar did not show category %d as selected", groceries)
	}
	// Month navigation must carry the category, or stepping a month silently
	// widens the view to every category. The href is attribute-escaped.
	if want := "/transactions?month=2026-06&amp;category=" + strconv.FormatInt(groceries, 10); !strings.Contains(body, want) {
		t.Errorf("body missing prev-month link %q", want)
	}
}

// TestTransactionsFilterBarNarrowsAccount covers the filter bar on an account
// view: category narrows within the selected account rather than replacing it.
func TestTransactionsFilterBarNarrowsAccount(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	chequing, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	visa, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Visa", Type: store.TypeChecking})
	gid, _ := s.CreateGroup(ctx, uid, "Everyday", 0)
	groceries, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Groceries"})
	dining, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Dining"})

	mkTx(t, s, uid, chequing, groceries, "2026-07-04", "Loblaws")
	mkTx(t, s, uid, visa, groceries, "2026-07-09", "Costco")
	mkTx(t, s, uid, chequing, dining, "2026-07-05", "CornerCafe")

	resp, err := client.Get(ts.URL + "/transactions?account=" + strconv.FormatInt(chequing, 10) +
		"&month=2026-07&category=" + strconv.FormatInt(groceries, 10))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)

	if !strings.Contains(body, "Loblaws") {
		t.Error("body missing Loblaws; want the account+category intersection")
	}
	// Same category, other account: excluded because the account filter stands.
	if strings.Contains(body, "Costco") {
		t.Error("body contains Costco; category must narrow within the account, not replace it")
	}
	// Same account, other category.
	if strings.Contains(body, "CornerCafe") {
		t.Error("body contains CornerCafe; want it filtered out by category")
	}
	// The bar's hidden field carries the account so the next pick keeps it.
	if want := `name="account" value="` + strconv.FormatInt(chequing, 10) + `"`; !strings.Contains(body, want) {
		t.Errorf("filter bar missing hidden account field %q", want)
	}
}

// TestTransactionsUnfilteredListsMonth pins the filter bar's "All categories"
// end state. With neither filter the page lists every account's transactions
// for the month; it deliberately no longer redirects to the budget.
func TestTransactionsUnfilteredListsMonth(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	chequing, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	visa, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Visa", Type: store.TypeChecking})
	gid, _ := s.CreateGroup(ctx, uid, "Everyday", 0)
	groceries, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Groceries"})
	dining, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Dining"})

	mkTx(t, s, uid, chequing, groceries, "2026-07-04", "Loblaws")
	mkTx(t, s, uid, visa, dining, "2026-07-09", "CornerCafe")
	mkTx(t, s, uid, chequing, groceries, "2026-06-04", "OldMart")

	resp, err := client.Get(ts.URL + "/transactions?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unfiltered view no longer redirects)", resp.StatusCode)
	}
	body := readAll(t, resp)

	for _, want := range []string{"Loblaws", "CornerCafe"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; want every account and category for the month", want)
		}
	}
	if strings.Contains(body, "OldMart") {
		t.Error("body contains OldMart; the month filter must still apply")
	}
	if !strings.Contains(body, "All categories") {
		t.Error("filter bar missing the All categories option")
	}
}

// TestQuickAddPreselectsFilteredCategory checks that entering a transaction from
// a category-filtered view starts with that category selected. The behaviour
// used to live in the "new transaction" sheet; the always-open quick-add row
// replaced it, and the preselect has to come with it.
func TestQuickAddPreselectsFilteredCategory(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Everyday", 0)
	groceries, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Groceries"})
	dining, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Dining"})

	body := readAll(t, mustGetOK(t, client,
		ts.URL+"/transactions?category="+strconv.FormatInt(groceries, 10)+"&month=2026-07"))

	// Scope to the quick-add's own category picker: the filter bar above it also
	// marks the filtered category selected, so an unscoped match would pass even
	// if the quick-add ignored the filter entirely.
	qa := selectboxScope(t, body, "qa-category")
	if !strings.Contains(qa, selectboxSelected(groceries)) {
		t.Errorf("quick-add did not preselect the filtered category %d", groceries)
	}
	if strings.Contains(qa, selectboxSelected(dining)) {
		t.Errorf("quick-add preselected category %d, which is not the filtered one", dining)
	}
}

// selectboxScope returns just the markup of one selectbox — from its trigger id
// up to the next selectbox container — so an assertion can target a single
// picker on a page that renders several over the same option list.
func selectboxScope(t *testing.T, body, id string) string {
	t.Helper()
	i := strings.Index(body, `id="`+id+`"`)
	if i < 0 {
		t.Fatalf("selectbox %q not found", id)
	}
	rest := body[i:]
	if j := strings.Index(rest[1:], "select-container"); j >= 0 {
		return rest[:j+1]
	}
	return rest
}

func TestTransactionEditTypeLock(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	a, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: store.TypeChecking})
	b, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Savings", Type: store.TypeChecking})

	// A plain expense can't be turned into a transfer on edit.
	expID, _ := s.CreateTransaction(ctx, uid, store.Transaction{
		Date: time.Now(), AccountID: a, OutflowCents: 2500,
	})
	resp := putForm(t, client, ts.URL+"/transactions/"+strconv.FormatInt(expID, 10), url.Values{
		"tx_type": {"transfer"}, "account_id": {strconv.FormatInt(a, 10)},
		"transfer_to": {strconv.FormatInt(b, 10)}, "date": {"2026-07-10"}, "amount": {"25.00"},
	})
	if resp != http.StatusBadRequest {
		t.Errorf("expense→transfer: status = %d, want 400", resp)
	}

	// A transfer can't be turned into a plain expense on edit.
	legID, _, _ := s.CreateTransfer(ctx, uid, store.TransferInput{
		Date: time.Now(), FromAccountID: a, ToAccountID: b, AmountCents: 1500,
	})
	resp = putForm(t, client, ts.URL+"/transactions/"+strconv.FormatInt(legID, 10), url.Values{
		"tx_type": {"expense"}, "account_id": {strconv.FormatInt(a, 10)},
		"date": {"2026-07-10"}, "amount": {"15.00"},
	})
	if resp != http.StatusBadRequest {
		t.Errorf("transfer→expense: status = %d, want 400", resp)
	}
}

func putForm(t *testing.T, client *http.Client, url string, form url.Values) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
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

func TestBudgetGroupsReorder(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	ctx := context.Background()
	a, _ := s.CreateGroup(ctx, uid, "Alpha", 1)
	b, _ := s.CreateGroup(ctx, uid, "Bravo", 2)
	c, _ := s.CreateGroup(ctx, uid, "Charlie", 3)

	ids := strconv.FormatInt(c, 10) + "," + strconv.FormatInt(a, 10) + "," + strconv.FormatInt(b, 10)
	resp, err := client.PostForm(ts.URL+"/budget/groups/reorder", url.Values{"ids": {ids}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	groups, _ := s.ListGroups(ctx, uid)
	var names []string
	for _, g := range groups {
		if g.Name == "Income" {
			continue
		}
		names = append(names, g.Name)
	}
	if got, want := strings.Join(names, ","), "Charlie,Alpha,Bravo"; got != want {
		t.Errorf("group order = %q, want %q", got, want)
	}

	// A non-numeric id is rejected.
	bad, _ := client.PostForm(ts.URL+"/budget/groups/reorder", url.Values{"ids": {"1,x,2"}})
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("bad id status = %d, want 400", bad.StatusCode)
	}
}

func TestBudgetCategoriesReorder(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	// An account is required, or /budget renders the first-run screen.
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: "checking"}); err != nil {
		t.Fatal(err)
	}
	g1, _ := s.CreateGroup(ctx, uid, "G1", 1)
	g2, _ := s.CreateGroup(ctx, uid, "G2", 2)
	a, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: g1, Name: "A", SortOrder: 0})
	b, _ := s.CreateCategory(ctx, uid, store.Category{GroupID: g1, Name: "B", SortOrder: 1})

	// Drag A from G1 into G2: destination G2=[A], source G1=[B].
	form := url.Values{
		"month":  {"2026-07"},
		"groups": {strconv.FormatInt(g2, 10) + "|" + strconv.FormatInt(g1, 10)},
		"cats":   {strconv.FormatInt(a, 10) + "|" + strconv.FormatInt(b, 10)},
	}
	resp, err := client.PostForm(ts.URL+"/budget/categories/reorder", form)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// The affected group headers come back as OOB swaps.
	if body := readAll(t, resp); !strings.Contains(body, "group-head-"+strconv.FormatInt(g2, 10)) {
		t.Errorf("response missing OOB header for destination group %d", g2)
	}

	cats, _ := s.ListCategories(ctx, uid, false)
	groupOf := func(id int64) int64 {
		for _, c := range cats {
			if c.ID == id {
				return c.GroupID
			}
		}
		return -1
	}
	if groupOf(a) != g2 {
		t.Errorf("A group = %d, want %d (G2)", groupOf(a), g2)
	}
	if groupOf(b) != g1 {
		t.Errorf("B group = %d, want %d (G1)", groupOf(b), g1)
	}
}

// Income and Credit were collapsible sections inside the budget table until the
// redesign moved them into panels (BudgetIncomePanel / BudgetCreditPanel). Their
// collapse cookies and the tests covering them went with them; only per-group
// collapse survives, covered below.

func TestGroupCollapseRendering(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	u, _ := url.Parse(ts.URL)

	// An account is required, or /budget renders the first-run screen.
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: "checking"}); err != nil {
		t.Fatal(err)
	}
	g1, _ := s.CreateGroup(ctx, uid, "G1", 1)
	g2, _ := s.CreateGroup(ctx, uid, "G2", 2)

	// The single budget_groups_collapsed cookie lists collapsed group ids; only
	// g1 is collapsed here.
	client.Jar.SetCookies(u, []*http.Cookie{{
		Name: "budget_groups_collapsed", Value: strconv.FormatInt(g1, 10), Path: "/",
	}})
	resp, err := client.Get(ts.URL + "/budget?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)

	// Read each group's class attribute rather than matching the whole string:
	// the group div carries layout classes alongside the collapse marker, so an
	// exact-match assertion would break on any styling change.
	if cls := groupClass(t, body, g1); !strings.Contains(cls, "group-collapsed") {
		t.Errorf("group %d should render collapsed, class = %q", g1, cls)
	}
	if cls := groupClass(t, body, g2); strings.Contains(cls, "group-collapsed") {
		t.Errorf("group %d should render expanded, class = %q", g2, cls)
	}
}

// currentRowLabel returns the visible text of the account-overview row carrying
// aria-current, so a test can say which row is highlighted rather than only how
// many are.
func currentRowLabel(t *testing.T, body string) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)aria-current="page"\s*>.*?<span[^>]*>([^<]*)</span>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// groupClass extracts the class attribute of the budget group div with the
// given id.
func groupClass(t *testing.T, body string, gid int64) string {
	t.Helper()
	re := regexp.MustCompile(`id="group-` + strconv.FormatInt(gid, 10) + `" class="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("group %d not found in body", gid)
	}
	return m[1]
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

// mkTx inserts a categorized expense, identified in assertions by its payee.
func mkTx(t *testing.T, s *store.Store, uid, acct, cat int64, date, payee string) {
	t.Helper()
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatal(err)
	}
	p := payee
	if _, err := s.CreateTransaction(context.Background(), uid, store.Transaction{
		Date: d, AccountID: acct, CategoryID: &cat, Payee: &p, OutflowCents: 1000,
	}); err != nil {
		t.Fatal(err)
	}
}

// selectboxSelected is the markup templUI emits for a chosen selectbox option.
// Asserting on it couples these tests to templUI's DOM, but it is the only
// signal that distinguishes "selected" from merely "present in the list".
func selectboxSelected(id int64) string {
	return `data-tui-selectbox-value="` + strconv.FormatInt(id, 10) + `" data-tui-selectbox-selected="true"`
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
