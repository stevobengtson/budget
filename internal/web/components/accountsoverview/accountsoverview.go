package accountsoverview

import "github.com/sbengtson/budget/internal/core/store"

// bucket is one display group in the overview (Cash / Credit / Loan).
// TotalCents is display-signed: liabilities are negated so debt reads as a
// positive "amount owing" (see displayCents).
type bucket struct {
	Name       string
	Accounts   []store.AccountWithBalance
	TotalCents int64
}

// displayCents returns the amount to show for a row. Liability accounts
// (credit/loan) store a negative balance when in debt; negate so the list
// shows a positive amount owing.
func displayCents(a store.AccountWithBalance) int64 {
	if a.Type.IsLiability() {
		return -a.BalanceCents
	}
	return a.BalanceCents
}

// group bins accounts into Cash (checking/savings/cash), Credit, and Loan
// buckets in that fixed order, dropping any empty bucket. netWorthCents is the
// sum of raw stored balances (assets minus liabilities, naturally signed).
// Archived accounts are excluded so the summary reflects only active accounts,
// regardless of whether the caller passed them in.
func group(accts []store.AccountWithBalance) (buckets []bucket, netWorthCents int64) {
	cash := bucket{Name: "Cash"}
	credit := bucket{Name: "Credit"}
	loan := bucket{Name: "Loan"}

	for _, a := range accts {
		if a.ArchivedAt != nil {
			continue
		}
		netWorthCents += a.BalanceCents
		switch a.Type {
		case store.TypeCredit:
			credit.Accounts = append(credit.Accounts, a)
			credit.TotalCents += displayCents(a)
		case store.TypeLoan:
			loan.Accounts = append(loan.Accounts, a)
			loan.TotalCents += displayCents(a)
		default: // checking, savings, cash
			cash.Accounts = append(cash.Accounts, a)
			cash.TotalCents += displayCents(a)
		}
	}

	for _, b := range []bucket{cash, credit, loan} {
		if len(b.Accounts) > 0 {
			buckets = append(buckets, b)
		}
	}
	return buckets, netWorthCents
}
