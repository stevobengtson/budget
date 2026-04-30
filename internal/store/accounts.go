package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type AccountType string

const (
	TypeChecking AccountType = "checking"
	TypeSavings  AccountType = "savings"
	TypeCash     AccountType = "cash"
	TypeCredit   AccountType = "credit"
	TypeLoan     AccountType = "loan"
)

func AllAccountTypes() []AccountType {
	return []AccountType{TypeChecking, TypeSavings, TypeCash, TypeCredit, TypeLoan}
}

func (a AccountType) IsLiability() bool { return a == TypeCredit || a == TypeLoan }

type Account struct {
	ID                   int64
	Name                 string
	Type                 AccountType
	StartingBalanceCents int64
	CreditLimitCents     *int64
	AprBps               *int64
	MonthlyPaymentCents  *int64
	IncludeInPaydown     bool
	PaymentCategoryID    *int64
	ArchivedAt           *time.Time
	CreatedAt            time.Time
}

type AccountWithBalance struct {
	Account
	BalanceCents int64
}

func (s *Store) CreateAccount(ctx context.Context, a Account) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO accounts(name, type, starting_balance_cents, credit_limit_cents, apr_bps,
		                     monthly_payment_cents, include_in_paydown, payment_category_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, string(a.Type), a.StartingBalanceCents,
		nullInt(a.CreditLimitCents), nullInt(a.AprBps),
		nullInt(a.MonthlyPaymentCents), a.IncludeInPaydown, nullInt(a.PaymentCategoryID),
	)
	if err != nil {
		return 0, fmt.Errorf("create account: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAccount(ctx context.Context, a Account) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE accounts
		 SET name=?, type=?, starting_balance_cents=?, credit_limit_cents=?, apr_bps=?,
		     monthly_payment_cents=?, include_in_paydown=?, payment_category_id=?
		 WHERE id=?`,
		a.Name, string(a.Type), a.StartingBalanceCents,
		nullInt(a.CreditLimitCents), nullInt(a.AprBps),
		nullInt(a.MonthlyPaymentCents), a.IncludeInPaydown, nullInt(a.PaymentCategoryID), a.ID,
	)
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	return nil
}

func (s *Store) ArchiveAccount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET archived_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

func (s *Store) UnarchiveAccount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET archived_at=NULL WHERE id=?`, id)
	return err
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
	return err
}

// ListAccounts returns accounts with their computed balance. If includeArchived
// is false, archived accounts are filtered out.
func (s *Store) ListAccounts(ctx context.Context, includeArchived bool) ([]AccountWithBalance, error) {
	q := `
SELECT a.id, a.name, a.type, a.starting_balance_cents, a.credit_limit_cents, a.apr_bps,
       a.monthly_payment_cents, a.include_in_paydown, a.payment_category_id,
       a.archived_at, a.created_at,
       a.starting_balance_cents
         + COALESCE((SELECT SUM(inflow_cents)  FROM transactions t WHERE t.account_id=a.id), 0)
         - COALESCE((SELECT SUM(outflow_cents) FROM transactions t WHERE t.account_id=a.id), 0) AS balance
FROM accounts a`
	if !includeArchived {
		q += ` WHERE a.archived_at IS NULL`
	}
	q += ` ORDER BY a.archived_at IS NOT NULL, a.name`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AccountWithBalance
	for rows.Next() {
		var a AccountWithBalance
		var typ string
		var lim, apr, pay, payCat sql.NullInt64
		var include bool
		var archived nullTime
		if err := rows.Scan(&a.ID, &a.Name, &typ, &a.StartingBalanceCents, &lim, &apr,
			&pay, &include, &payCat, &archived, &a.CreatedAt, &a.BalanceCents); err != nil {
			return nil, err
		}
		a.Type = AccountType(typ)
		a.CreditLimitCents = intPtr(lim)
		a.AprBps = intPtr(apr)
		a.MonthlyPaymentCents = intPtr(pay)
		a.IncludeInPaydown = include
		a.PaymentCategoryID = intPtr(payCat)
		a.ArchivedAt = archived.Ptr()
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAccount(ctx context.Context, id int64) (Account, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, starting_balance_cents, credit_limit_cents, apr_bps,
		        monthly_payment_cents, include_in_paydown, payment_category_id,
		        archived_at, created_at
		 FROM accounts WHERE id=?`, id)
	var a Account
	var typ string
	var lim, apr, pay, payCat sql.NullInt64
	var archived nullTime
	if err := row.Scan(&a.ID, &a.Name, &typ, &a.StartingBalanceCents, &lim, &apr,
		&pay, &a.IncludeInPaydown, &payCat, &archived, &a.CreatedAt); err != nil {
		return Account{}, err
	}
	a.Type = AccountType(typ)
	a.CreditLimitCents = intPtr(lim)
	a.AprBps = intPtr(apr)
	a.MonthlyPaymentCents = intPtr(pay)
	a.PaymentCategoryID = intPtr(payCat)
	a.ArchivedAt = archived.Ptr()
	return a, nil
}
