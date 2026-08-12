package accountsoverview

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/store"
)

func acct(name string, t store.AccountType, bal int64) store.AccountWithBalance {
	return store.AccountWithBalance{
		Account:      store.Account{Name: name, Type: t},
		BalanceCents: bal,
	}
}

func TestGroupBucketsAndNetWorth(t *testing.T) {
	accts := []store.AccountWithBalance{
		acct("Spending", store.TypeChecking, 117855),
		acct("Savings", store.TypeSavings, 63481),
		acct("Visa", store.TypeCredit, -4256004),
		acct("Auto Loan", store.TypeLoan, -1067574),
	}

	buckets, net := group(accts)

	if len(buckets) != 3 {
		t.Fatalf("want 3 buckets, got %d", len(buckets))
	}
	if buckets[0].Key != "cash" || buckets[1].Key != "credit" || buckets[2].Key != "loan" {
		t.Fatalf("bucket order wrong: %s/%s/%s", buckets[0].Key, buckets[1].Key, buckets[2].Key)
	}
	if got := buckets[0].TotalCents; got != 181336 { // 117855 + 63481
		t.Errorf("cash total = %d, want 181336", got)
	}
	if got := buckets[1].TotalCents; got != 4256004 { // liability shown positive
		t.Errorf("credit total = %d, want 4256004", got)
	}
	if got := buckets[2].TotalCents; got != 1067574 {
		t.Errorf("loan total = %d, want 1067574", got)
	}
	if net != -5142242 { // 181336 - 4256004 - 1067574
		t.Errorf("net worth = %d, want -5142242", net)
	}
}

func TestGroupOmitsEmptyBuckets(t *testing.T) {
	accts := []store.AccountWithBalance{
		acct("Cash on hand", store.TypeCash, 5000),
		acct("Visa", store.TypeCredit, -10000),
	}
	buckets, _ := group(accts)
	if len(buckets) != 2 {
		t.Fatalf("want 2 buckets (no Loan), got %d", len(buckets))
	}
	if buckets[0].Key != "cash" || buckets[1].Key != "credit" {
		t.Fatalf("unexpected buckets: %s/%s", buckets[0].Key, buckets[1].Key)
	}
}

func TestDisplayCents(t *testing.T) {
	if got := displayCents(acct("chk", store.TypeChecking, 12345)); got != 12345 {
		t.Errorf("asset display = %d, want 12345", got)
	}
	if got := displayCents(acct("visa", store.TypeCredit, -12345)); got != 12345 {
		t.Errorf("liability display = %d, want 12345", got)
	}
}

func TestAccountsOverviewRenders(t *testing.T) {
	accts := []store.AccountWithBalance{
		acct("Spending", store.TypeChecking, 117855),
		acct("Visa", store.TypeCredit, -4256004),
	}
	var buf bytes.Buffer
	if err := AccountsOverview(accts, ViewContext{Month: "2026-07"}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Note: templ HTML-escapes & to &amp; in the href attribute; assert the
	// stable prefix rather than the full query string.
	for _, want := range []string{"Spending", "$1,178.55", "Visa", "$42,560.04", "Est. Net Worth", "Add Account"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
	// With no selection nothing is marked current.
	if strings.Contains(out, `aria-current="page"`) {
		t.Error("unselected overview should not mark any account as current")
	}
}

// TestAccountsOverviewHighlightsSelected covers the sidebar highlight for the
// transactions page's ?account= filter: exactly one row is marked current and
// carries the active styling.
func TestAccountsOverviewHighlightsSelected(t *testing.T) {
	spending := acct("Spending", store.TypeChecking, 117855)
	spending.ID = 7
	visa := acct("Visa", store.TypeCredit, -4256004)
	visa.ID = 9

	var buf bytes.Buffer
	sel := int64(9)
	if err := AccountsOverview([]store.AccountWithBalance{spending, visa}, ViewContext{Month: "2026-07", AccountID: &sel}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if got := strings.Count(out, `aria-current="page"`); got != 1 {
		t.Errorf("aria-current count = %d, want exactly 1", got)
	}
	if !strings.Contains(out, "bg-sidebar-accent") {
		t.Error("selected row missing the active background class")
	}
	// The selected row must be Visa, not Spending. Slice at the marker and check
	// which name follows it.
	i := strings.Index(out, `aria-current="page"`)
	if i < 0 {
		t.Fatal("no current row rendered")
	}
	rest := out[i:]
	if vi, si := strings.Index(rest, "Visa"), strings.Index(rest, "Spending"); vi < 0 || (si >= 0 && si < vi) {
		t.Error("aria-current is on the wrong account row; want Visa")
	}
}

// TestAccountsOverviewUnknownSelection guards against a stale or foreign
// account id (e.g. an archived account still in the URL) highlighting nothing
// rather than erroring or defaulting to the first row.
func TestAccountsOverviewUnknownSelection(t *testing.T) {
	a := acct("Spending", store.TypeChecking, 117855)
	a.ID = 7

	var buf bytes.Buffer
	sel := int64(404)
	if err := AccountsOverview([]store.AccountWithBalance{a}, ViewContext{Month: "2026-07", AccountID: &sel}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `aria-current="page"`) {
		t.Error("an id matching no account should highlight nothing")
	}
}

func TestAccountsOverviewEmptyState(t *testing.T) {
	var buf bytes.Buffer
	if err := AccountsOverview(nil, ViewContext{Month: "2026-07"}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "No accounts yet") {
		t.Error("empty overview should show the no-accounts message")
	}
	// The Add Account button remains the call to action.
	if !strings.Contains(out, "Add Account") {
		t.Error("empty overview should still offer Add Account")
	}
	// Net worth is hidden until there's at least one account.
	if strings.Contains(out, "Est. Net Worth") {
		t.Error("empty overview should not show the net-worth line")
	}
}

func TestTxHref(t *testing.T) {
	cat := int64(5)
	// url.Values.Encode sorts keys, so params come out alphabetically.
	cases := []struct {
		name string
		view ViewContext
		want string
	}{
		{"month only", ViewContext{Month: "2026-07"}, "/transactions?account=3&month=2026-07"},
		{"no filters", ViewContext{}, "/transactions?account=3"},
		{"month and category", ViewContext{Month: "2026-07", CategoryID: &cat}, "/transactions?account=3&category=5&month=2026-07"},
		{"category only", ViewContext{CategoryID: &cat}, "/transactions?account=3&category=5"},
	}
	for _, tc := range cases {
		if got := txHref(3, tc.view); got != tc.want {
			t.Errorf("%s: txHref = %q, want %q", tc.name, got, tc.want)
		}
	}

	// The account being linked to always wins over whatever the view context
	// carried — switching accounts must actually switch accounts.
	other := int64(99)
	got := txHref(3, ViewContext{AccountID: &other})
	if got != "/transactions?account=3" {
		t.Errorf("link account = %q, want the target account 3", got)
	}

	// Month arrives from the HX-Current-URL header, so a crafted value must not
	// be able to append its own params — Gin reads the first account=.
	got = txHref(3, ViewContext{Month: "2026-07&account=99"})
	if strings.Contains(got, "&account=99") {
		t.Errorf("month injected an extra param: %q", got)
	}
	if q, err := url.ParseQuery(strings.TrimPrefix(got, "/transactions?")); err != nil {
		t.Fatalf("unparseable href %q: %v", got, err)
	} else if len(q["account"]) != 1 || q.Get("account") != "3" {
		t.Errorf("account params = %v, want exactly [3]", q["account"])
	}
}
