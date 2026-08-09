package web

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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

// TestProgressBarCountsRolloverInTheEnvelope is the rendered counterpart to
// TestProgressPct: it proves the row passes Available, not Assigned, to the
// progress note. A category carrying $60 forward with $5 assigned and $1 spent
// has a $65 envelope; reading it against the assignment alone said
// "$1.00 of $5.00" while the Available column correctly showed $64.
func TestProgressBarCountsRolloverInTheEnvelope(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	acct, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	gid, err := s.CreateGroup(ctx, uid, "Transportation", 1)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := s.CreateCategory(ctx, uid, store.Category{
		GroupID: gid, Name: "Parking", RolloverMode: store.RolloverCarry,
	})
	if err != nil {
		t.Fatal(err)
	}
	// June assigns $60 and spends none of it, so $60 carries into July.
	if err := s.SetAssigned(ctx, uid, "2026-06", cat, 6000); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, "2026-07", cat, 500); err != nil {
		t.Fatal(err)
	}
	d, _ := time.Parse("2006-01-02", "2026-07-03")
	payee := "Meter"
	if _, err := s.CreateTransaction(ctx, uid, store.Transaction{
		Date: d, AccountID: acct, CategoryID: &cat, Payee: &payee, OutflowCents: 100,
	}); err != nil {
		t.Fatal(err)
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-07"))
	if !strings.Contains(body, "$1.00 of $65.00") {
		t.Error(`progress note should read "$1.00 of $65.00" — carryover plus this month's assignment`)
	}
	if strings.Contains(body, "$1.00 of $5.00") {
		t.Error("progress note is measuring against the assignment alone, ignoring rollover")
	}
	if !strings.Contains(body, "$64.00") {
		t.Error("Available should be $64.00")
	}
}

// TestRailSeparatesOverspendFromCardBalances covers the split between the two
// rail cards. "Needs attention" is only for money spent past what was in a
// category — something to correct. A card balance is a normal state of affairs
// and lives in its own card, so carrying one no longer makes an otherwise fine
// month report a problem.
func TestRailSeparatesOverspendFromCardBalances(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()

	chq, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	visa, err := s.CreateAccount(ctx, uid, store.Account{Name: "Visa", Type: store.TypeCredit})
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Living", 1)
	gas, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Gas"})
	if err != nil {
		t.Fatal(err)
	}
	// Fund Gas so spending on the card does not also overspend it.
	if err := s.SetAssigned(ctx, uid, "2026-07", gas, 20000); err != nil {
		t.Fatal(err)
	}
	spend := func(acct int64, payee string, cents int64) {
		t.Helper()
		d, _ := time.Parse("2006-01-02", "2026-07-02")
		if _, err := s.CreateTransaction(ctx, uid, store.Transaction{
			Date: d, AccountID: acct, CategoryID: &gas, Payee: &payee, OutflowCents: cents,
		}); err != nil {
			t.Fatal(err)
		}
	}
	state := func() (attention, cards bool) {
		t.Helper()
		body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-07"))
		return strings.Contains(body, "Needs attention"), strings.Contains(body, "On your cards")
	}

	if a, c := state(); a || c {
		t.Errorf("clean month: attention=%v cards=%v, want both false", a, c)
	}

	// A card balance on its own is not a problem to fix.
	spend(visa, "Dinner", 1577)
	if a, c := state(); a || !c {
		t.Errorf("card balance only: attention=%v cards=%v, want false/true", a, c)
	}

	// Overspending is, and appears independently.
	spend(chq, "Pump", 25000)
	if a, c := state(); !a || !c {
		t.Errorf("overspent + card: attention=%v cards=%v, want both true", a, c)
	}
}

// TestPagerCountsVisibleTransferRows guards the interaction between one-row
// transfers and pagination. A transfer is stored as two legs but rendered as
// one, so the legs must be collapsed BEFORE the page is sliced — otherwise the
// pager counts rows nobody can see and a page of 50 stored rows renders however
// many of them happen to survive the filter.
func TestPagerCountsVisibleTransferRows(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	a, err := s.CreateAccount(ctx, uid, store.Account{Name: "A", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateAccount(ctx, uid, store.Account{Name: "B", Type: store.TypeSavings})
	if err != nil {
		t.Fatal(err)
	}
	// 30 transfers = 60 stored legs, which straddles the 50-row page size while
	// the 30 visible rows fit comfortably on one page.
	const n = 30
	for i := 0; i < n; i++ {
		if _, err := client.PostForm(ts.URL+"/transactions", url.Values{
			"tx_type": {"transfer"}, "date": {"2026-07-04"}, "amount": {"10.00"},
			"account_id": {itoa(a)}, "transfer_to": {itoa(b)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := s.ListTransactions(ctx, uid, store.TxFilter{Month: "2026-07"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2*n {
		t.Fatalf("stored legs = %d, want %d — transfers should still be two rows", len(stored), 2*n)
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/transactions?month=2026-07"))
	// Count row ids specifically — `id="tx-` alone also matches tx-quickadd and
	// tx-rows.
	if got := len(regexp.MustCompile(`id="tx-\d+"`).FindAllString(body, -1)); got != n {
		t.Errorf("page renders %d rows, want %d (one per transfer)", got, n)
	}
	if strings.Contains(body, "page 1 of") {
		t.Error("pager shown for 30 visible rows; the page holds 50")
	}
}

// TestStillInEnvelopesCountsRollover is the rail's counterpart to the progress
// bar's envelope arithmetic. Available is carryover + assigned − spent, so
// "Still in envelopes" is the sum of Available — computing assigned − spent
// instead silently omits every dollar rolled over from earlier months.
func TestStillInEnvelopesCountsRollover(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	acct, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Transportation", 1)
	cat, err := s.CreateCategory(ctx, uid, store.Category{
		GroupID: gid, Name: "Parking", RolloverMode: store.RolloverCarry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, "2026-06", cat, 6000); err != nil { // carries forward
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, "2026-07", cat, 500); err != nil {
		t.Fatal(err)
	}
	d, _ := time.Parse("2006-01-02", "2026-07-03")
	payee := "Meter"
	if _, err := s.CreateTransaction(ctx, uid, store.Transaction{
		Date: d, AccountID: acct, CategoryID: &cat, Payee: &payee, OutflowCents: 100,
	}); err != nil {
		t.Fatal(err)
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-07"))
	i := strings.Index(body, "Still in envelopes")
	if i < 0 {
		t.Fatal("rail is missing the Still in envelopes row")
	}
	row := body[i:min(i+300, len(body))]
	if !strings.Contains(row, "$64.00") {
		t.Errorf("Still in envelopes should be $64.00 (carryover included); row was %q", row)
	}
	if strings.Contains(row, "$4.00") {
		t.Error("Still in envelopes is computing assigned − spent, dropping the carryover")
	}
}

// TestGoalColumnShowsTargetAndMonthlyNeed restores visibility the redesign had
// dropped: the old table had a Goal column, and after the rewrite a goal could
// be set from the row menu but never seen on the row. The column is read-only —
// editing stays in the menu's "Set a goal" panel.
func TestGoalColumnShowsTargetAndMonthlyNeed(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Savings Goals", 1)
	withGoal, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Vacation"})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Rainy Day"})
	if err != nil {
		t.Fatal(err)
	}

	cats, _ := s.ListCategories(ctx, uid, false)
	for _, c := range cats {
		if c.ID != withGoal {
			continue
		}
		goal := int64(300000)
		due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
		c.GoalCents, c.GoalDueDate = &goal, &due
		if err := s.UpdateCategory(ctx, uid, c); err != nil {
			t.Fatal(err)
		}
	}

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-07"))

	// The column exists, between Category and Assigned.
	heads := body[strings.Index(body, ">Category<"):]
	gi, ai := strings.Index(heads, ">Goal<"), strings.Index(heads, ">Assigned<")
	if gi < 0 || ai < 0 || gi > ai {
		t.Errorf("Goal head should sit between Category and Assigned (goal=%d assigned=%d)", gi, ai)
	}

	goalRow := rowMarkup(t, body, withGoal)
	for _, want := range []string{"$3,000.00", "by Dec 2026", "/mo"} {
		if !strings.Contains(goalRow, want) {
			t.Errorf("goal row missing %q", want)
		}
	}
	// A category without a goal shows an empty cell, not a stray amount.
	if got := rowMarkup(t, body, plain); strings.Contains(got, "/mo") {
		t.Error("a category with no goal should show nothing in the Goal column")
	}
}

// rowMarkup returns the markup of one budget row, from its data-category-id.
func rowMarkup(t *testing.T, body string, catID int64) string {
	t.Helper()
	i := strings.Index(body, `data-category-id="`+itoa(catID)+`"`)
	if i < 0 {
		t.Fatalf("row for category %d not rendered", catID)
	}
	return body[i:min(i+2500, len(body))]
}

// TestGoalDueDateIsOptionalAndUsesThePicker covers the goal editor's date field:
// it uses templUI's calendar rather than the browser's native date input, starts
// empty, round-trips a chosen date, and can be cleared back to none — the
// picker has no clear of its own, so the editor supplies one.
func TestGoalDueDateIsOptionalAndUsesThePicker(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Savings Goals", 1)
	cat, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Vacation"})
	if err != nil {
		t.Fatal(err)
	}

	editor := func() string {
		return readAll(t, mustGetOK(t, client,
			ts.URL+"/budget/goal/"+itoa(cat)+"/edit?month=2026-07"))
	}
	// Match the whole tag: templ orders attributes alphabetically, so anchoring
	// on one attribute and scanning forward can miss the others.
	hiddenTag := regexp.MustCompile(`<input[^>]*data-tui-datepicker-hidden-input[^>]*>`)
	dueValue := func(body string) string {
		m := regexp.MustCompile(`name="goal_due" value="([^"]*)"`).FindStringSubmatch(hiddenTag.FindString(body))
		if m == nil {
			t.Fatal("goal_due hidden input not found")
		}
		return m[1]
	}

	first := editor()
	if strings.Contains(first, `type="date"`) {
		t.Error("goal editor should use the templUI calendar, not a native date input")
	}
	if !strings.Contains(first, "data-tui-datepicker") {
		t.Error("goal editor is missing the templUI datepicker")
	}
	if got := dueValue(first); got != "" {
		t.Errorf("a new goal should start with no date, got %q", got)
	}

	// A date is stored and comes back when the editor reopens.
	if _, err := client.PostForm(ts.URL+"/budget/goal/"+itoa(cat)+"?month=2026-07",
		url.Values{"goal_cents": {"3000.00"}, "goal_due": {"2026-12-01"}}); err != nil {
		t.Fatal(err)
	}
	if got := dueValue(editor()); got != "2026-12-01" {
		t.Errorf("reopened editor shows due %q, want 2026-12-01", got)
	}

	// Clearing the date keeps the goal — this is what the clear button posts.
	if _, err := client.PostForm(ts.URL+"/budget/goal/"+itoa(cat)+"?month=2026-07",
		url.Values{"goal_cents": {"3000.00"}, "goal_due": {""}}); err != nil {
		t.Fatal(err)
	}
	cats, _ := s.ListCategories(ctx, uid, false)
	for _, c := range cats {
		if c.ID != cat {
			continue
		}
		if c.GoalDueDate != nil {
			t.Errorf("due date should be cleared, got %v", c.GoalDueDate)
		}
		if c.GoalCents == nil || *c.GoalCents != 300000 {
			t.Error("clearing the date should not clear the goal amount")
		}
	}
}

// TestGoalNeedPreviewsBeforeSaving covers the goal editor's "to arrive on time"
// figure. It used to render only once a goal had been saved, so setting a new
// one gave no feedback until after the fact. It is now always present and
// refreshed from the server as the target and date change — computed by the same
// store.MonthlyTarget the saved row uses, so preview and result agree.
func TestGoalNeedPreviewsBeforeSaving(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Entertainment", 1)
	cat, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Streaming"})
	if err != nil {
		t.Fatal(err)
	}

	// The block is there before anything is saved, showing a dash rather than a
	// misleading $0.00/mo — and "/mo" is not doubled (format.GoalFor supplies it).
	editor := readAll(t, mustGetOK(t, client,
		ts.URL+"/budget/goal/"+itoa(cat)+"/edit?month=2026-08"))
	if !strings.Contains(editor, "To arrive on time") {
		t.Error("editor should always show the to-arrive-on-time block")
	}
	if strings.Contains(editor, "/mo/mo") {
		t.Error(`"/mo" is doubled — format.GoalFor already appends it`)
	}

	amount := regexp.MustCompile(`\$[\d,.]+/mo|—`)
	preview := func(goal, due string) string {
		t.Helper()
		resp, err := client.PostForm(ts.URL+"/budget/goal/"+itoa(cat)+"/preview?month=2026-08",
			url.Values{"goal_cents": {goal}, "goal_due": {due}})
		if err != nil {
			t.Fatal(err)
		}
		return amount.FindString(readAll(t, resp))
	}

	got := preview("149.99", "2027-08-01")
	if got == "" || got == "—" {
		t.Fatalf("preview with a goal and a date should compute a figure, got %q", got)
	}
	// No deadline means nothing to pace against; no amount means no goal.
	if p := preview("149.99", ""); p != "—" {
		t.Errorf("preview without a date = %q, want a dash", p)
	}
	if p := preview("", "2027-08-01"); p != "—" {
		t.Errorf("preview without an amount = %q, want a dash", p)
	}

	// Saving the previewed goal produces the same figure on the row.
	if _, err := client.PostForm(ts.URL+"/budget/goal/"+itoa(cat)+"?month=2026-08",
		url.Values{"goal_cents": {"149.99"}, "goal_due": {"2027-08-01"}}); err != nil {
		t.Fatal(err)
	}
	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-08"))
	if saved := amount.FindString(rowMarkup(t, body, cat)); saved != got {
		t.Errorf("saved row shows %q but the preview said %q — they must agree", saved, got)
	}
}

// TestGoalEditorPreviewTargetsItself guards an htmx inheritance trap. hx-target
// is inherited from ancestors, and the goal editor's form targets the whole
// panel — so the live preview, sitting inside that form, must set hx-target
// itself or its response replaces the editor with the bare figure.
func TestGoalEditorPreviewTargetsItself(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cat := seedGoalCategory(t, s, uid)

	editor := readAll(t, mustGetOK(t, client,
		ts.URL+"/budget/goal/"+itoa(cat)+"/edit?month=2026-08"))
	tag := regexp.MustCompile(`<div[^>]*id="goal-need-\d+"[^>]*>`).FindString(editor)
	if tag == "" {
		t.Fatal("preview element not rendered")
	}
	if !strings.Contains(tag, `hx-target="this"`) {
		t.Errorf("preview must set hx-target explicitly or it inherits the form's; got %s", tag)
	}
}

// TestSetAGoalIsIdempotent covers re-opening the editor. The menu inserts with
// afterend, so without a guard choosing "Set a goal" twice stacks two editors
// under the row.
func TestSetAGoalIsIdempotent(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cat := seedGoalCategory(t, s, uid)

	body := readAll(t, mustGetOK(t, client, ts.URL+"/budget?month=2026-08"))
	item := regexp.MustCompile(`<button[^>]*goal/` + itoa(cat) + `/edit[^>]*>`).FindString(body)
	if item == "" {
		t.Fatal("Set a goal menu item not rendered")
	}
	// Attribute values are HTML-escaped, so compare the decoded form.
	decoded := html.UnescapeString(item)
	if !strings.Contains(decoded, "getElementById('goal-edit-"+itoa(cat)+"')?.remove()") {
		t.Errorf("Set a goal should drop any open editor before inserting; got %s", decoded)
	}
}

func seedGoalCategory(t *testing.T, s *store.Store, uid int64) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Chequing", Type: store.TypeChecking}); err != nil {
		t.Fatal(err)
	}
	gid, _ := s.CreateGroup(ctx, uid, "Entertainment", 1)
	cat, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Streaming"})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}
