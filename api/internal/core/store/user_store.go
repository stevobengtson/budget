package store

import (
	"context"
	"fmt"
)

// LocalUserID is the sentinel user that owns rows created outside the
// multi-user API — notably the single-user TUI. It matches the user_id column
// DEFAULT in migration 00007, so TUI inserts that omit user_id are attributed
// to this user automatically.
const LocalUserID = "00000000-0000-0000-0000-000000000001"

// UserStore is a Store scoped to a single user. It intentionally does NOT
// embed *Store: only the methods defined here are reachable, so an unscoped
// query cannot be called by accident through a UserStore (it would not
// compile). Scope additional resources by adding methods here.
type UserStore struct {
	store  *Store
	userID string
}

// For returns a view of the store scoped to userID. Every query issued through
// the returned UserStore is constrained to that user's rows.
func (s *Store) For(userID string) *UserStore {
	return &UserStore{store: s, userID: userID}
}

// UserID returns the user this store is scoped to.
func (u *UserStore) UserID() string { return u.userID }

// ListAccounts returns the user's accounts with computed balances. The balance
// subqueries also constrain to the user's transactions.
func (u *UserStore) ListAccounts(ctx context.Context, includeArchived bool) ([]AccountWithBalance, error) {
	q := `
SELECT a.id, a.name, a.type, a.starting_balance_cents, a.credit_limit_cents, a.apr_bps,
       a.monthly_payment_cents, a.include_in_paydown, a.payment_category_id,
       a.archived_at, a.created_at,
       a.starting_balance_cents
         + COALESCE((SELECT SUM(inflow_cents)  FROM transactions t WHERE t.account_id=a.id AND t.user_id=a.user_id), 0)
         - COALESCE((SELECT SUM(outflow_cents) FROM transactions t WHERE t.account_id=a.id AND t.user_id=a.user_id), 0) AS balance
FROM accounts a
WHERE a.user_id = ?`
	if !includeArchived {
		q += ` AND a.archived_at IS NULL`
	}
	q += ` ORDER BY a.archived_at IS NOT NULL, a.name`

	rows, err := u.store.queryAll(ctx, q, u.userID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AccountWithBalance
	for rows.Next() {
		a, err := scanAccountWithBalance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAccount returns a single account owned by the user, or sql.ErrNoRows if it
// does not exist or belongs to another user.
func (u *UserStore) GetAccount(ctx context.Context, id int64) (Account, error) {
	row := u.store.queryOne(ctx,
		`SELECT id, name, type, starting_balance_cents, credit_limit_cents, apr_bps,
		        monthly_payment_cents, include_in_paydown, payment_category_id,
		        archived_at, created_at
		 FROM accounts WHERE id=? AND user_id=?`, id, u.userID)
	return scanAccount(row)
}

// CreateAccount inserts an account owned by the user and returns its id.
func (u *UserStore) CreateAccount(ctx context.Context, a Account) (int64, error) {
	id, err := u.store.insertReturningID(ctx,
		`INSERT INTO accounts(user_id, name, type, starting_balance_cents, credit_limit_cents, apr_bps,
		                     monthly_payment_cents, include_in_paydown, payment_category_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.userID, a.Name, string(a.Type), a.StartingBalanceCents,
		nullInt(a.CreditLimitCents), nullInt(a.AprBps),
		nullInt(a.MonthlyPaymentCents), a.IncludeInPaydown, nullInt(a.PaymentCategoryID),
	)
	if err != nil {
		return 0, fmt.Errorf("create account: %w", err)
	}
	return id, nil
}
