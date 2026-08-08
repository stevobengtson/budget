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
// page has a real row to render. It also creates an account: with none, /budget
// renders the first-run screen instead of the budget.
func seedCategory(t *testing.T, s *store.Store, uid int64, month string, cents int64) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: "checking"}); err != nil {
		t.Fatal(err)
	}
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

// TestFirstRunShownUntilAnAccountExists covers the new empty state. Nothing can
// be budgeted until money exists somewhere, so a user with no accounts gets the
// first-run screen; adding one account replaces it with the real budget.
func TestFirstRunShownUntilAnAccountExists(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget"))
	if !strings.Contains(body, "Let&#39;s find out what your money is doing.") {
		t.Error("a user with no accounts should see the first-run screen")
	}
	if strings.Contains(body, `id="budget-rail"`) {
		t.Error("first run should not render the budget rail")
	}

	if _, err := s.CreateAccount(context.Background(), uid,
		store.Account{Name: "Checking", Type: "checking"}); err != nil {
		t.Fatal(err)
	}
	body = readAll(t, mustGetOK(t, client, ts.URL+"/budget"))
	if !strings.Contains(body, `id="budget-rail"`) {
		t.Error("once an account exists the budget should render")
	}
	if strings.Contains(body, "Let&#39;s find out what your money is doing.") {
		t.Error("first-run screen should be gone once an account exists")
	}
}

// TestFirstRunRedirectsIntoBudget checks the first-run form's distinct response:
// there is no modal to close, so the handler sends the browser to /budget.
func TestFirstRunRedirectsIntoBudget(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, _ := serveAuthed(t, s)

	resp, err := client.PostForm(ts.URL+"/accounts", url.Values{
		"first_run":        {"1"},
		"name":             {"Main Checking"},
		"type":             {"checking"},
		"starting_balance": {"500.00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("HX-Redirect"); got != "/budget" {
		t.Errorf("HX-Redirect = %q, want /budget", got)
	}
}

// TestPaydownSourceIsWorded guards the redesign's most pointed copy change: the
// schedule's source column said "✓ spent" / "→ assigned" / "· default", carrying
// its meaning in a glyph and a colour. It now names the category in words.
func TestPaydownSourceIsWorded(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	if err := s.SetAddOnEnabled(ctx, uid, "paydown", true); err != nil {
		t.Fatal(err)
	}
	apr := int64(2124)
	if _, err := s.CreateAccount(ctx, uid, store.Account{
		Name: "Chase Sapphire", Type: "credit", StartingBalanceCents: -288661,
		AprBps: &apr, IncludeInPaydown: true,
	}); err != nil {
		t.Fatal(err)
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/paydown"))
	if !strings.Contains(body, "default payment") {
		t.Error("schedule should word the payment source")
	}
	for _, glyph := range []string{"✓ spent", "→ assigned", "· default"} {
		if strings.Contains(body, glyph) {
			t.Errorf("schedule still renders the old glyph %q", glyph)
		}
	}
	// The three stat cards.
	for _, m := range []string{"Going to debt each month", "Debt-free", "Interest over"} {
		if !strings.Contains(body, m) {
			t.Errorf("paydown page missing stat card %q", m)
		}
	}
}

// TestAssignSheetOpensAndSaves covers the mobile assign path. Below 768px the
// inline Assigned field is hidden and the Available figure opens this sheet
// instead; the sheet posts to the same endpoint, so the response is the usual
// row + out-of-band rail refresh.
func TestAssignSheetOpensAndSaves(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cid := seedCategory(t, s, uid, "2026-07", 10000)

	sheet := readAll(t, mustGetOK(t, client,
		ts.URL+"/budget/assign/"+itoa(cid)+"/sheet?month=2026-07"))
	for _, m := range []string{"Assign to Gas", "Same as last month", "Empty it", "left to assign"} {
		if !strings.Contains(sheet, m) {
			t.Errorf("assign sheet missing %q", m)
		}
	}

	// Saving from the sheet uses the ordinary assign endpoint.
	resp, err := client.PostForm(ts.URL+"/budget/assign/"+itoa(cid)+"?month=2026-07",
		url.Values{"amount": {"75.00"}})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, `id="budget-rail"`) || !strings.Contains(body, "$75.00") {
		t.Error("assign from the sheet should return the row plus the rail")
	}
}

// TestBudgetRowHidesInlineAssignOnMobile is the structural half of the same
// change: the inline field and the sheet trigger must both exist, each scoped to
// its own breakpoint, or one viewport ends up with no way to assign at all.
func TestBudgetRowHidesInlineAssignOnMobile(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cid := seedCategory(t, s, uid, "2026-07", 10000)

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-07"))
	if !strings.Contains(body, `class="js-assign hidden items-center justify-end md:flex"`) {
		t.Error("inline assign form should be desktop-only")
	}
	if !strings.Contains(body, "/budget/assign/"+itoa(cid)+"/sheet") {
		t.Error("row should offer the assign sheet as the mobile path")
	}
}

// TestSidebarTriggerTargetsItsWrapper guards a failure that produces no error at
// all: templUI's sidebar Trigger reads the sidebar id from context (published by
// sidebar.Layout) to build data-tui-sidebar-target, while the wrapper's
// data-tui-sidebar-id comes from sidebar.Sidebar. Set an explicit ID on only one
// of them and they disagree — toggleSidebar finds no wrapper, and the collapse
// button silently does nothing.
func TestSidebarTriggerTargetsItsWrapper(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	if _, err := s.CreateAccount(context.Background(), uid,
		store.Account{Name: "Checking", Type: "checking"}); err != nil {
		t.Fatal(err)
	}
	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget"))

	wrapper := regexp.MustCompile(`data-tui-sidebar-id="([^"]*)"`).FindStringSubmatch(body)
	if wrapper == nil {
		t.Fatal("no sidebar wrapper rendered")
	}
	targets := regexp.MustCompile(`data-tui-sidebar-target="([^"]*)"`).FindAllStringSubmatch(body, -1)
	if len(targets) == 0 {
		t.Fatal("no sidebar trigger rendered")
	}
	for _, m := range targets {
		if m[1] != wrapper[1] {
			t.Errorf("trigger targets %q but the wrapper is %q — collapse would do nothing",
				m[1], wrapper[1])
		}
	}
}

// TestTransferCategoryDoesNotCollide covers the two category fields that now
// coexist in a transaction form — the plain one and the transfer-only one. Both
// are always in the DOM (CSS shows one per type) and a display:none select still
// posts, so they carry distinct names and the transfer field is authoritative on
// a transfer. Without that, a category left over from expense mode lands on the
// transfer leg.
func TestTransferCategoryDoesNotCollide(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	chk, err := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateAccount(ctx, uid, store.Account{Name: "Visa", Type: store.TypeCredit})
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Living", 1)
	cat, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Card Payment"})
	if err != nil {
		t.Fatal(err)
	}

	post := func(day, amount string, plain, transferCat string) {
		t.Helper()
		if _, err := client.PostForm(ts.URL+"/transactions", url.Values{
			"tx_type": {"transfer"}, "date": {day}, "amount": {amount},
			"account_id": {itoa(chk)}, "transfer_to": {itoa(card)},
			"category_id": {plain}, "transfer_category_id": {transferCat},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Tagged deliberately: the category is stored.
	post("2026-07-04", "120.00", "", itoa(cat))
	// Stale expense value, no transfer category chosen: must NOT be stored.
	post("2026-07-05", "10.00", itoa(cat), "")

	rows, err := s.ListTransactions(ctx, uid, store.TxFilter{Month: "2026-07"})
	if err != nil {
		t.Fatal(err)
	}
	var tagged, leaked int
	for _, r := range rows {
		if r.CategoryID == nil {
			continue
		}
		if r.Date.Day() == 4 {
			tagged++
		}
		if r.Date.Day() == 5 {
			leaked++
		}
	}
	if tagged != 1 {
		t.Errorf("tagged legs = %d, want 1 (the spending leg)", tagged)
	}
	if leaked != 0 {
		t.Errorf("a stale expense category leaked onto %d transfer leg(s)", leaked)
	}
}

// TestTransferEditsInline covers the 4b interaction: a transfer edits in place
// with both accounts on show, because a side sheet would hide the list that
// gives a two-account record its context. Ordinary transactions still open the
// sheet, so the row menu has to choose the right editor per record.
func TestTransferEditsInline(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	from, err := s.CreateAccount(ctx, uid, store.Account{Name: "Main Checking", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	to, err := s.CreateAccount(ctx, uid, store.Account{Name: "High-Yield Savings", Type: store.TypeSavings})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PostForm(ts.URL+"/transactions", url.Values{
		"tx_type": {"transfer"}, "date": {"2026-07-04"}, "amount": {"300.00"},
		"account_id": {itoa(from)}, "transfer_to": {itoa(to)},
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ListTransactions(ctx, uid, store.TxFilter{Month: "2026-07"})
	var leg store.Transaction
	for _, r := range rows {
		if r.OutflowCents > 0 {
			leg = r
		}
	}

	// The row menu offers the inline editor for a transfer, not the sheet.
	list := readAll(t, mustGetOK(t, client, ts.URL+"/transactions?month=2026-07"))
	if !strings.Contains(list, "/edit-row") {
		t.Error("transfer row should offer the inline editor")
	}

	// Opening it yields a form carrying the row's own id, so Cancel can swap the
	// row back into the same place.
	editor := readAll(t, mustGetOK(t, client, ts.URL+"/transactions/"+itoa(leg.ID)+"/edit-row"))
	for _, m := range []string{
		"Editing a transfer", `id="tx-` + itoa(leg.ID) + `"`,
		`name="account_id"`, `name="transfer_to"`, "data-tx-swap",
		"/transactions/" + itoa(leg.ID) + "/row", // Cancel
	} {
		if !strings.Contains(editor, m) {
			t.Errorf("inline transfer editor missing %q", m)
		}
	}

	// Cancel restores the plain row.
	row := readAll(t, mustGetOK(t, client, ts.URL+"/transactions/"+itoa(leg.ID)+"/row"))
	if !strings.Contains(row, `id="tx-`+itoa(leg.ID)+`"`) || strings.Contains(row, "Editing a transfer") {
		t.Error("row endpoint should return the plain row")
	}

	// Ordinary transactions edit inline too — one idiom for the whole list.
	acct2, _ := s.CreateAccount(ctx, uid, store.Account{Name: "Cash", Type: store.TypeChecking})
	if _, err := client.PostForm(ts.URL+"/transactions", url.Values{
		"tx_type": {"expense"}, "date": {"2026-07-06"}, "amount": {"5.00"},
		"account_id": {itoa(acct2)}, "payee": {"Kiosk"},
	}); err != nil {
		t.Fatal(err)
	}
	rows2, _ := s.ListTransactions(ctx, uid, store.TxFilter{Month: "2026-07"})
	for _, r := range rows2 {
		if r.TransferAccountID != nil || r.Payee == nil {
			continue
		}
		resp, err := client.Get(ts.URL + "/transactions/" + itoa(r.ID) + "/edit-row")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("edit-row on an ordinary transaction = %d, want 200", resp.StatusCode)
		}
	}
}
