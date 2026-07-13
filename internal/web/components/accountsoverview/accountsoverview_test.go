package accountsoverview

import (
	"bytes"
	"context"
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
	if buckets[0].Name != "Cash" || buckets[1].Name != "Credit" || buckets[2].Name != "Loan" {
		t.Fatalf("bucket order wrong: %s/%s/%s", buckets[0].Name, buckets[1].Name, buckets[2].Name)
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
	if buckets[0].Name != "Cash" || buckets[1].Name != "Credit" {
		t.Fatalf("unexpected buckets: %s/%s", buckets[0].Name, buckets[1].Name)
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
	if err := AccountsOverview(accts).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Accounts Overview", "Spending", "$1,178.55", "Visa", "$42,560.04", "Est. Net Worth"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}
