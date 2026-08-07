package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/store"
)

// seedCategory creates one group + category with an assignment, so the budget
// page has a real row to render.
func seedCategory(t *testing.T, s *store.Store, uid int64, month string, cents int64) int64 {
	t.Helper()
	ctx := context.Background()
	gid, err := s.CreateGroup(ctx, uid, "Living", 1)
	if err != nil {
		t.Fatal(err)
	}
	cid, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Gas"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, month, cid, cents); err != nil {
		t.Fatal(err)
	}
	return cid
}

// TestBudgetRendersAsGridNotTable guards the redesign's core structural change:
// the six-column <table> became a four-column div grid with the totals lifted
// out into the summary rail.
func TestBudgetRendersAsGridNotTable(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	seedCategory(t, s, uid, "2026-07", 10000)

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-07"))
	for _, m := range []string{
		`id="budget-rail"`,   // summary rail present
		"Ready to assign",    // headline figure
		"Still in envelopes", // totals card
		`id="budget-groups"`, // drag-sort container
		"bg-progress-track",  // the row's progress bar
		`data-category-id=`,  // rows still carry the sort/collapse hooks
	} {
		if !strings.Contains(body, m) {
			t.Errorf("budget page missing %q", m)
		}
	}
	if strings.Contains(body, "<table") {
		t.Error("budget page should no longer render a <table>")
	}
}

// TestAssignRefreshesRailOutOfBand is the redesign's key wiring check. An
// assignment moves Assigned, Available, Still-in-envelopes and Ready-to-assign;
// the first two live in the swapped row, the last two only in the rail. Before
// the redesign the rail's job was done by footer rows inside the table, so this
// verifies the out-of-band target moved with them.
func TestAssignRefreshesRailOutOfBand(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cid := seedCategory(t, s, uid, "2026-07", 10000)

	resp, err := client.PostForm(
		ts.URL+"/budget/assign/"+itoa(cid)+"?month=2026-07",
		url.Values{"amount": {"250.00"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)

	if !strings.Contains(body, `id="cat-`+itoa(cid)+`"`) {
		t.Error("assign response should swap the edited category row")
	}
	if !strings.Contains(body, `id="budget-rail"`) || !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("assign response should refresh the summary rail out of band")
	}
	if !strings.Contains(body, "$250.00") {
		t.Errorf("assign response missing the new amount; got %d bytes", len(body))
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestQuickAddCreatesAndReturnsRows covers the redesign's central interaction:
// the always-open row posts straight to /transactions and gets the refreshed
// list back, with no sheet in between.
func TestQuickAddCreatesAndReturnsRows(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	acct, err := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: "checking"})
	if err != nil {
		t.Fatal(err)
	}

	// The quick-add row is present on the page, not behind a button.
	page := readAll(t, mustGetOK(t, client, ts.URL+"/transactions?all=1"))
	if !strings.Contains(page, `id="tx-quickadd"`) {
		t.Fatal("transactions page missing the always-open quick-add row")
	}

	// htmx sends HX-Current-URL on every request; renderTxRows reads it to scope
	// the returned list to the month and filters currently on screen. Without it
	// the server falls back to the current month and the new row is filtered out.
	body := readAll(t, postHTMX(t, client, ts.URL+"/transactions",
		"/transactions?month=2026-07", url.Values{
			"tx_type":    {"expense"},
			"date":       {"2026-07-04"},
			"amount":     {"12.34"},
			"payee":      {"Corner Shop"},
			"account_id": {itoa(acct)},
		}))
	if !strings.Contains(body, "Corner Shop") {
		t.Errorf("quick-add response missing the new row; got %d bytes", len(body))
	}
	if !strings.Contains(body, "$12.34") {
		t.Error("quick-add response missing the amount")
	}
}

// TestTransferRendersAsOneRow covers the "one row, two readings" rule. Both legs
// are still stored; only the rendering collapses. Unfiltered, the arriving leg
// is suppressed so a transfer never appears twice and money reads as leaving.
func TestTransferRendersAsOneRow(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	from, err := s.CreateAccount(ctx, uid, store.Account{Name: "Main Checking", Type: "checking"})
	if err != nil {
		t.Fatal(err)
	}
	to, err := s.CreateAccount(ctx, uid, store.Account{Name: "High-Yield Savings", Type: "savings"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PostForm(ts.URL+"/transactions", url.Values{
		"tx_type":     {"transfer"},
		"date":        {"2026-07-04"},
		"amount":      {"300.00"},
		"account_id":  {itoa(from)},
		"transfer_to": {itoa(to)},
	}); err != nil {
		t.Fatal(err)
	}

	// Both legs are stored.
	rows, err := s.ListTransactions(ctx, uid, store.TxFilter{Month: "2026-07"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 stored legs, got %d", len(rows))
	}

	// Unfiltered: exactly one row renders.
	body := readAll(t, mustGetOK(t, client, ts.URL+"/transactions?month=2026-07"))
	if n := strings.Count(body, "transfer</span>"); n != 1 {
		t.Errorf("unfiltered list should show the transfer once, showed %d", n)
	}

	// Scoped to the destination: that account's own leg renders, as an inflow.
	body = readAll(t, mustGetOK(t, client, ts.URL+"/transactions?month=2026-07&account="+itoa(to)))
	if n := strings.Count(body, "transfer</span>"); n != 1 {
		t.Errorf("destination-scoped list should show the transfer once, showed %d", n)
	}
	if !strings.Contains(body, "text-money-positive") {
		t.Error("destination-scoped transfer should read as an inflow")
	}
}

// postHTMX posts a form the way htmx does, including the HX-Current-URL header
// that tells the server which view the request came from.
func postHTMX(t *testing.T, client *http.Client, endpoint, currentURL string, vals url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(vals.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", currentURL)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestQuickAddHidesAccountWhenFiltered checks the account field is dropped from
// the quick-add row when the list is already scoped to one account — the answer
// is the account you are looking at. It is hidden rather than removed, because a
// display:none select still posts its value, and it must come back as "From"
// when the type is a transfer.
func TestQuickAddHidesAccountWhenFiltered(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	acct, err := s.CreateAccount(context.Background(), uid, store.Account{Name: "Checking", Type: "checking"})
	if err != nil {
		t.Fatal(err)
	}

	unfiltered := readAll(t, mustGetOK(t, client, ts.URL+"/transactions?all=1"))
	if strings.Contains(accountFieldClass(t, unfiltered), "tx-transfer-only") {
		t.Error("unfiltered list should show the Account field in every mode")
	}

	filtered := readAll(t, mustGetOK(t, client, ts.URL+"/transactions?all=1&account="+itoa(acct)))
	if !strings.Contains(accountFieldClass(t, filtered), "tx-transfer-only") {
		t.Error("account-filtered list should hide Account except in transfer mode")
	}
	// Still posts: the select is in the DOM with the filtered account selected.
	if !strings.Contains(filtered, `name="account_id"`) {
		t.Error("account_id must remain in the form so it still posts")
	}
}

// accountFieldClass returns the class attribute of the quick-add row's account
// field wrapper. Matched by id because the "To" field carries the same
// visibility class.
func accountFieldClass(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`id="qa-account-field" class="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("quick-add account field not found")
	}
	return m[1]
}
