package store

import (
	"context"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// newTestPostgresStore opens the shared Postgres test database (see
// openTestDB), truncates + re-seeds it, and returns a store owned by a fresh
// user. It is a thin alias over newTestStoreUser retained so the TestPostgres*
// cases keep their descriptive names.
func newTestPostgresStore(t *testing.T) (*Store, int64) {
	t.Helper()
	return newTestStoreUser(t)
}

func TestPostgresAccountsCRUDAndBalance(t *testing.T) {
	s, uid := newTestPostgresStore(t)
	ctx := context.Background()

	id, err := s.CreateAccount(ctx, uid, Account{
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
	visaID, err := s.CreateAccount(ctx, uid, Account{
		Name: "Visa", Type: TypeCredit,
		CreditLimitCents: &limit, AprBps: &apr,
	})
	if err != nil {
		t.Fatalf("create visa: %v", err)
	}

	accs, err := s.ListAccounts(ctx, uid, false)
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
	s, uid := newTestPostgresStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, uid, "Monthly", 0)
	groc, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})

	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &groc, OutflowCents: 8_000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, month, groc, 20_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, uid, month)
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

// TestPostgresSeedNewUser exercises the second-user registration path on real
// Postgres. SeedNewUser inserts is_income=TRUE; an earlier version bound the
// integer literal 1, which SQLite accepts but Postgres rejects for a BOOLEAN
// column — so this bug was invisible to the SQLite tests.
func TestPostgresSeedNewUser(t *testing.T) {
	s, _ := newTestPostgresStore(t) // helper already created the owner
	ctx := context.Background()

	uid2, err := s.CreateUser(ctx, "second@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SeedNewUser(ctx, uid2, i18n.EnCA, nil); err != nil {
		t.Fatalf("SeedNewUser on postgres: %v", err)
	}
	cats, err := s.ListCategories(ctx, uid2, false)
	if err != nil {
		t.Fatal(err)
	}
	income := 0
	for _, c := range cats {
		if c.IsIncome {
			income++
		}
	}
	if income != 1 {
		t.Fatalf("second user income categories = %d, want 1", income)
	}
}
