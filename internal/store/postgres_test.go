package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/db"
)

// Skip the test unless BUDGET_POSTGRES_URL is set, e.g.:
//   BUDGET_POSTGRES_URL=postgres://postgres:postgres@localhost:5432/budget_test
// The test wipes all known tables to give itself a clean slate.
func newTestPostgresStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("BUDGET_POSTGRES_URL")
	if url == "" {
		t.Skip("BUDGET_POSTGRES_URL not set; skipping live Postgres test")
	}
	conn, dialect, err := db.Open(url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if dialect != db.DialectPostgres {
		t.Fatalf("expected postgres dialect, got %v", dialect)
	}

	// Wipe all tables (cascading delete via TRUNCATE).
	for _, table := range []string{"transactions", "budgets", "categories", "category_groups", "incomes", "accounts"} {
		if _, err := conn.Exec("TRUNCATE TABLE " + table + " RESTART IDENTITY CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}

	// Re-seed Income system category that migration 00005 created.
	var gid int64
	if err := conn.QueryRow(
		`INSERT INTO category_groups(name, sort_order) VALUES ('Income', -100) RETURNING id`).Scan(&gid); err != nil {
		t.Fatalf("seed income group: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO categories(group_id, name, is_income, sort_order) VALUES ($1, 'Income', TRUE, 0)`, gid); err != nil {
		t.Fatalf("seed income category: %v", err)
	}

	return NewWithDialect(conn, DialectPostgres)
}

func TestPostgresAccountsCRUDAndBalance(t *testing.T) {
	s := newTestPostgresStore(t)
	ctx := context.Background()

	id, err := s.CreateAccount(ctx, Account{
		Name: "Checking", Type: TypeChecking, StartingBalanceCents: 100_000,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero account id")
	}

	limit := int64(500_000)
	apr := int64(1999)
	visaID, err := s.CreateAccount(ctx, Account{
		Name: "Visa", Type: TypeCredit,
		CreditLimitCents: &limit, AprBps: &apr,
	})
	if err != nil {
		t.Fatalf("create visa: %v", err)
	}

	accs, err := s.ListAccounts(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accs))
	}
	for _, a := range accs {
		if a.ID == id && a.BalanceCents != 100_000 {
			t.Errorf("checking balance = %d, want 100000", a.BalanceCents)
		}
		if a.ID == visaID && a.CreditLimitCents == nil {
			t.Errorf("credit limit not preserved")
		}
	}
}

func TestPostgresMonthBudgetAndStrftime(t *testing.T) {
	s := newTestPostgresStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, "Monthly", 0)
	groc, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Groceries"})

	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: chk, CategoryID: &groc, OutflowCents: 8_000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, month, groc, 20_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	r := findCategoryRow(t, rows, "Groceries")
	if r.SpentCents != 8_000 {
		t.Errorf("spent = %d, want 8000", r.SpentCents)
	}
	if r.AssignedCents != 20_000 {
		t.Errorf("assigned = %d, want 20000", r.AssignedCents)
	}
	if r.AvailableCents != 12_000 {
		t.Errorf("available = %d, want 12000", r.AvailableCents)
	}
}

// Make sure the package doesn't pull a sql.Conn that breaks unrelated tests.
var _ = sql.ErrNoRows
