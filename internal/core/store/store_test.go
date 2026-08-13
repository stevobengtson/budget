package store

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/db"
)

// testDSN returns the Postgres DSN for the shared test database. Postgres is
// required — if BUDGET_POSTGRES_URL is unset we default to the local budget_test
// database rather than skipping.
func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

// testTables lists every table truncated between tests, ordered so a single
// TRUNCATE ... CASCADE resets identities cleanly.
var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "sessions", "users",
	"estimate_incomes", "estimate_categories", "estimate_groups", "estimates",
}

// testDBLockKey is a global Postgres advisory-lock key. `go test ./...` runs
// each package's test binary in parallel, and they all share the one
// budget_test database, so every DB-backed test grabs this lock for its whole
// duration — serializing access so tests don't clobber each other's data.
const testDBLockKey = 918273645

// openTestDB opens the shared Postgres test database, holds a global advisory
// lock for the life of the test, applies migrations, then truncates every table
// and re-seeds the global Income group/category that migration 00005 creates
// (NULL user_id, exactly as the migration leaves it). The returned pool is
// closed on cleanup, releasing the lock.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Dedicated single-connection pool that holds the advisory lock, kept apart
	// from the store's own pool so store queries aren't constrained to one conn.
	lockConn, _, err := db.Open(testDSN(), false)
	if err != nil {
		t.Fatalf("open lock conn: %v", err)
	}
	lockConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = lockConn.Close() })
	if _, err := lockConn.Exec("SELECT pg_advisory_lock($1)", testDBLockKey); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}

	conn, dialect, err := db.Open(testDSN(), false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Migrate under the lock so concurrent first-run migrations can't race.
	if err := db.MigrateUp(conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := conn.Exec("TRUNCATE TABLE " + strings.Join(testTables, ", ") + " RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var gid int64
	if err := conn.QueryRow(
		`INSERT INTO category_groups(name, sort_order) VALUES ('Income', -100) RETURNING id`).Scan(&gid); err != nil {
		t.Fatalf("seed income group: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO categories(group_id, name, is_income, sort_order) VALUES ($1, 'Income', TRUE, 0)`, gid); err != nil {
		t.Fatalf("seed income category: %v", err)
	}
	return conn
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(openTestDB(t))
}

// newTestStoreUser returns a store plus a verified user id that owns the
// migration-seeded rows (previously NULL-owner), for exercising the
// user-scoped store methods. ClaimOrphanData hands the seeded Income
// category/group to this user so ListCategories(uid) still surfaces it.
func TestUserNameDefaults(t *testing.T) {
	s, uid := newTestStoreUser(t)
	u, err := s.GetUserByID(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "User Name" {
		t.Errorf("default name = %q, want %q", u.Name, "User Name")
	}
}

func newTestStoreUser(t *testing.T) (*Store, int64) {
	t.Helper()
	s := newTestStore(t)
	uid, err := s.CreateUser(context.Background(), "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := s.ClaimOrphanData(context.Background(), uid); err != nil {
		t.Fatalf("claim orphan: %v", err)
	}
	return s, uid
}

func TestAccountsCRUDAndBalance(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	id, err := s.CreateAccount(ctx, uid, Account{
		Name: "Checking", Type: TypeChecking, StartingBalanceCents: 100_000,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
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
		t.Fatalf("list: %v", err)
	}
	if len(accs) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accs))
	}

	for _, a := range accs {
		switch a.ID {
		case id:
			if a.BalanceCents != 100_000 {
				t.Errorf("checking balance = %d, want 100000", a.BalanceCents)
			}
		case visaID:
			if a.BalanceCents != 0 {
				t.Errorf("visa balance = %d, want 0", a.BalanceCents)
			}
			if a.CreditLimitCents == nil || *a.CreditLimitCents != 500_000 {
				t.Errorf("credit limit not preserved")
			}
		}
	}
}

func TestTransactionsAffectBalance(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	visa, _ := s.CreateAccount(ctx, uid, Account{Name: "Visa", Type: TypeCredit})
	gid, _ := s.CreateGroup(ctx, uid, "Monthly", 0)
	groc, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})

	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Now(), AccountID: chk, CategoryID: &groc, OutflowCents: 8000,
	}); err != nil {
		t.Fatalf("tx1: %v", err)
	}
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Now(), AccountID: visa, CategoryID: &groc, OutflowCents: 5000,
	}); err != nil {
		t.Fatalf("tx2: %v", err)
	}

	accs, _ := s.ListAccounts(ctx, uid, false)
	for _, a := range accs {
		switch a.ID {
		case chk:
			if a.BalanceCents != 92_000 {
				t.Errorf("checking after spend = %d, want 92000", a.BalanceCents)
			}
		case visa:
			if a.BalanceCents != -5_000 {
				t.Errorf("visa after spend = %d, want -5000", a.BalanceCents)
			}
		}
	}
}

func TestTransfer(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	visa, _ := s.CreateAccount(ctx, uid, Account{Name: "Visa", Type: TypeCredit})

	out, in, err := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: visa, AmountCents: 10_000,
	})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if out == 0 || in == 0 {
		t.Fatalf("transfer ids missing")
	}

	accs, _ := s.ListAccounts(ctx, uid, false)
	for _, a := range accs {
		switch a.ID {
		case chk:
			if a.BalanceCents != 90_000 {
				t.Errorf("chk balance = %d, want 90000", a.BalanceCents)
			}
		case visa:
			if a.BalanceCents != 10_000 {
				t.Errorf("visa balance after payment = %d, want 10000", a.BalanceCents)
			}
		}
	}

	// Deleting one leg removes both.
	if err := s.DeleteTransaction(ctx, uid, out); err != nil {
		t.Fatalf("delete: %v", err)
	}
	txs, _ := s.ListTransactions(ctx, uid, TxFilter{})
	if len(txs) != 0 {
		t.Errorf("expected 0 txs after transfer delete, got %d", len(txs))
	}
}

func TestMonthBudget(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, uid, "Monthly", 0)
	groc, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})

	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// Spend $80 this month.
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &groc, OutflowCents: 8000,
	}); err != nil {
		t.Fatal(err)
	}
	// Assign $200 this month.
	if err := s.SetAssigned(ctx, uid, month, groc, 20_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, uid, month)
	if err != nil {
		t.Fatalf("month budget: %v", err)
	}
	r := findCategoryRow(t, rows, "Groceries")
	if r.AssignedCents != 20_000 {
		t.Errorf("assigned = %d, want 20000", r.AssignedCents)
	}
	if r.SpentCents != 8_000 {
		t.Errorf("spent = %d, want 8000", r.SpentCents)
	}
	if r.AvailableCents != 12_000 {
		t.Errorf("available = %d, want 12000", r.AvailableCents)
	}
}

// TestArchiveRoundTripPreservesAvailable is the guarantee the archive warning
// and the Archived panel both rest on: archiving hides a category's balance from
// the month but does not spend, move or destroy it, and unarchiving returns the
// same figure to MonthBudget.
func TestArchiveRoundTripPreservesAvailable(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, uid, "Monthly", 0)
	vac, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Vacation"})

	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)
	if err := s.SetAssigned(ctx, uid, month, vac, 20_000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &vac, OutflowCents: 7_550,
	}); err != nil {
		t.Fatal(err)
	}
	const want = 12_450 // 200.00 assigned − 75.50 spent

	if got := findCategoryRow(t, mustMonthBudget(t, s, uid, month), "Vacation").AvailableCents; got != want {
		t.Fatalf("available before archive = %d, want %d", got, want)
	}

	if err := s.ArchiveCategory(ctx, uid, vac); err != nil {
		t.Fatal(err)
	}

	// Gone from the month, so it counts toward no total on the budget page.
	for _, r := range mustMonthBudget(t, s, uid, month) {
		if r.CategoryName == "Vacation" {
			t.Error("archived category still in MonthBudget")
		}
	}

	// But the panel can still see it, and still see the money.
	archived, err := s.ListArchivedCategories(ctx, uid, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 {
		t.Fatalf("archived count = %d, want 1", len(archived))
	}
	if archived[0].ID != vac || archived[0].Name != "Vacation" || archived[0].GroupName != "Monthly" {
		t.Errorf("archived row = %+v, want Vacation in Monthly", archived[0])
	}
	if archived[0].AvailableCents != want {
		t.Errorf("archived available = %d, want %d", archived[0].AvailableCents, want)
	}
	if n, _ := s.CountArchivedCategories(ctx, uid); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}

	if err := s.UnarchiveCategory(ctx, uid, vac); err != nil {
		t.Fatal(err)
	}
	if got := findCategoryRow(t, mustMonthBudget(t, s, uid, month), "Vacation").AvailableCents; got != want {
		t.Errorf("available after unarchive = %d, want %d", got, want)
	}
	if n, _ := s.CountArchivedCategories(ctx, uid); n != 0 {
		t.Errorf("count after unarchive = %d, want 0", n)
	}
}

// TestArchivedBalanceSurvivesGroupDelete pins the one path that could otherwise
// destroy an archived balance: DeleteGroup hard-deletes the archived categories
// in the group. A category holding money necessarily has a budgets or
// transactions row referencing it, and those foreign keys have no ON DELETE
// clause, so the whole delete rolls back rather than taking the money with it.
func TestArchivedBalanceSurvivesGroupDelete(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	gid, _ := s.CreateGroup(ctx, uid, "Sinking", 0)
	vac, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Vacation"})

	month := "2026-04"
	// Assignment only — no transaction. This is the case where the balance rests
	// entirely on a budgets row.
	if err := s.SetAssigned(ctx, uid, month, vac, 12_450); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveCategory(ctx, uid, vac); err != nil {
		t.Fatal(err)
	}

	// The group now looks empty to the budget page (archived rows are filtered
	// out), so the "delete empty group" control is offered. It must not succeed.
	if err := s.DeleteGroup(ctx, uid, gid); err == nil {
		t.Fatal("deleting a group holding an archived balance should be rejected")
	}

	archived, err := s.ListArchivedCategories(ctx, uid, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].AvailableCents != 12_450 {
		t.Fatalf("archived after rejected delete = %+v, want one row holding 12450", archived)
	}
}

func mustMonthBudget(t *testing.T, s *Store, uid int64, month string) []CategoryBudget {
	t.Helper()
	rows, err := s.MonthBudget(context.Background(), uid, month)
	if err != nil {
		t.Fatalf("month budget: %v", err)
	}
	return rows
}

func TestMonthBudgetSpentNetsInflows(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, uid, "Monthly", 0)
	dining, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Dining Out"})

	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// Spend $80, then receive a $45 refund into the same category.
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &dining, OutflowCents: 8000,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &dining, InflowCents: 4500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, month, dining, 20_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, uid, month)
	if err != nil {
		t.Fatalf("month budget: %v", err)
	}
	r := findCategoryRow(t, rows, "Dining Out")
	// Spent is net of the refund so the row reconciles on screen:
	// assigned - spent = available.
	if r.SpentCents != 3_500 {
		t.Errorf("spent = %d, want 3500 (8000 outflow - 4500 inflow)", r.SpentCents)
	}
	if r.AvailableCents != 16_500 {
		t.Errorf("available = %d, want 16500", r.AvailableCents)
	}
	if r.AssignedCents-r.SpentCents != r.AvailableCents {
		t.Errorf("row does not reconcile: assigned(%d) - spent(%d) = %d, available = %d",
			r.AssignedCents, r.SpentCents, r.AssignedCents-r.SpentCents, r.AvailableCents)
	}
}

func findCategoryRow(t *testing.T, rows []CategoryBudget, name string) CategoryBudget {
	t.Helper()
	for _, r := range rows {
		if r.CategoryName == name {
			return r
		}
	}
	t.Fatalf("category %q not in MonthBudget rows", name)
	return CategoryBudget{}
}

func TestIncomesCRUDAndTotal(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	month := "2026-04"
	work, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: "Work", AmountCents: 980_000})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	if _, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: "Government", AmountCents: 6_600}); err != nil {
		t.Fatalf("create gov: %v", err)
	}
	if _, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: "Contract", AmountCents: 80_000}); err != nil {
		t.Fatalf("create contract: %v", err)
	}

	total, err := s.TotalIncome(ctx, uid, month)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1_066_600 {
		t.Errorf("total = %d, want 1066600", total)
	}

	rows, err := s.ListIncomes(ctx, uid, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}

	// Different month is isolated.
	if _, err := s.CreateIncome(ctx, uid, Income{Month: "2026-05", Name: "Work", AmountCents: 1}); err != nil {
		t.Fatal(err)
	}
	if total, _ := s.TotalIncome(ctx, uid, month); total != 1_066_600 {
		t.Errorf("total leaked across months: %d", total)
	}

	// Update + delete.
	if err := s.UpdateIncome(ctx, uid, Income{ID: work, Name: "Work", AmountCents: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	if total, _ := s.TotalIncome(ctx, uid, month); total != 1_086_600 {
		t.Errorf("after update, total = %d, want 1086600", total)
	}
	if err := s.DeleteIncome(ctx, uid, work); err != nil {
		t.Fatal(err)
	}
	if total, _ := s.TotalIncome(ctx, uid, month); total != 86_600 {
		t.Errorf("after delete, total = %d, want 86600", total)
	}
}

func TestIncomeCategorySeededAndLocked(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	cats, err := s.ListCategories(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	var income *Category
	for i, c := range cats {
		if c.IsIncome {
			income = &cats[i]
			break
		}
	}
	if income == nil {
		t.Fatal("Income category not seeded by migration")
	}

	if err := s.ArchiveCategory(ctx, uid, income.ID); err == nil {
		t.Error("ArchiveCategory should refuse income category")
	}
	if err := s.DeleteCategory(ctx, uid, income.ID); err == nil {
		t.Error("DeleteCategory should refuse income category")
	}
}

func TestActualIncomeForMonth(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking})

	// Find seeded Income.
	cats, _ := s.ListCategories(ctx, uid, false)
	var income int64
	for _, c := range cats {
		if c.IsIncome {
			income = c.ID
		}
	}

	now := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// Paycheck inflow categorized as Income.
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &income, InflowCents: 500_000,
	}); err != nil {
		t.Fatal(err)
	}
	// Refund inflow on a non-income category — must NOT count.
	gid, _ := s.CreateGroup(ctx, uid, "Misc", 0)
	other, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Refunds"})
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: chk, CategoryID: &other, InflowCents: 1_000,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ActualIncomeForMonth(ctx, uid, month)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500_000 {
		t.Errorf("actual income = %d, want 500000", got)
	}
}

func TestCreditCardActivityForMonth(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000_00})
	visa, _ := s.CreateAccount(ctx, uid, Account{Name: "Visa", Type: TypeCredit, StartingBalanceCents: -100_000})
	gid, _ := s.CreateGroup(ctx, uid, "Bills", 0)
	groc, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})

	now := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// $500 of purchases on Visa.
	_, _ = s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: visa, CategoryID: &groc, OutflowCents: 50_000,
	})
	// Bank-booked interest on Visa (no category) — should also count as
	// "purchase" since it raises the balance owed.
	_, _ = s.CreateTransaction(ctx, uid, Transaction{
		Date: now, AccountID: visa, OutflowCents: 4_500,
	})
	// $300 transfer from Checking → Visa (covers some of the spend).
	_, _, _ = s.CreateTransfer(ctx, uid, TransferInput{
		Date: now, FromAccountID: chk, ToAccountID: visa, AmountCents: 30_000,
	})

	// Activity from a different month must not leak.
	other := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	_, _ = s.CreateTransaction(ctx, uid, Transaction{
		Date: other, AccountID: visa, OutflowCents: 99_999,
	})

	rows, err := s.CreditCardActivityForMonth(ctx, uid, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 credit row, got %d", len(rows))
	}
	r := rows[0]
	if r.AccountID != visa {
		t.Errorf("wrong account id: %d", r.AccountID)
	}
	if r.PurchasesCents != 54_500 {
		t.Errorf("purchases = %d, want 54500", r.PurchasesCents)
	}
	if r.PaymentsCents != 30_000 {
		t.Errorf("payments = %d, want 30000", r.PaymentsCents)
	}
	if r.OwingCents != 24_500 {
		t.Errorf("owing = %d, want 24500", r.OwingCents)
	}
}

func TestCategorizedTransfer(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 200_000})
	limit := int64(500_000)
	apr := int64(2099)
	visa, _ := s.CreateAccount(ctx, uid, Account{
		Name: "Visa", Type: TypeCredit, CreditLimitCents: &limit, AprBps: &apr,
		StartingBalanceCents: -100_000, // owe $1000
	})
	gid, _ := s.CreateGroup(ctx, uid, "Bills", 0)
	ccPay, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "CC Payment"})
	if err := s.SetAssigned(ctx, uid, MonthKey(time.Now()), ccPay, 10_000); err != nil {
		t.Fatal(err)
	}

	// Interest charge on the card itself (no category — purely a balance event).
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Now(), AccountID: visa, OutflowCents: 4_500,
	}); err != nil {
		t.Fatal(err)
	}

	// Transfer 1: cover interest.
	if _, _, err := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: visa,
		AmountCents: 4_500, CategoryID: &ccPay,
	}); err != nil {
		t.Fatal(err)
	}
	// Transfer 2: pay down principal.
	if _, _, err := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: visa,
		AmountCents: 5_500, CategoryID: &ccPay,
	}); err != nil {
		t.Fatal(err)
	}

	// Visa balance: -100000 - 4500 (interest) + 4500 + 5500 = -94500.
	accs, _ := s.ListAccounts(ctx, uid, false)
	for _, a := range accs {
		if a.ID == visa && a.BalanceCents != -94_500 {
			t.Errorf("Visa balance = %d, want -94500", a.BalanceCents)
		}
		if a.ID == chk && a.BalanceCents != 200_000-4_500-5_500 {
			t.Errorf("Checking balance = %d, want %d", a.BalanceCents, 200_000-10_000)
		}
	}

	// Budget impact: CC Payment should be spent 10000 (4500 + 5500), available 0.
	rows, err := s.MonthBudget(ctx, uid, MonthKey(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rows {
		if r.CategoryID == ccPay {
			found = true
			if r.SpentCents != 10_000 {
				t.Errorf("CC Payment spent = %d, want 10000", r.SpentCents)
			}
			if r.AvailableCents != 0 {
				t.Errorf("CC Payment available = %d, want 0", r.AvailableCents)
			}
		}
	}
	if !found {
		t.Error("CC Payment row not found in MonthBudget")
	}

	// Deleting one leg of the categorized transfer must remove both legs.
	txs, _ := s.ListTransactions(ctx, uid, TxFilter{})
	var firstTransferID int64
	for _, tx := range txs {
		if tx.TransferAccountID != nil && tx.OutflowCents == 4_500 {
			firstTransferID = tx.ID
			break
		}
	}
	if firstTransferID == 0 {
		t.Fatal("could not locate categorized transfer")
	}
	if err := s.DeleteTransaction(ctx, uid, firstTransferID); err != nil {
		t.Fatal(err)
	}
	left, _ := s.ListTransactions(ctx, uid, TxFilter{})
	// 1 interest charge + 2 legs of remaining transfer = 3 rows.
	if len(left) != 3 {
		t.Errorf("after delete, %d rows remain, want 3", len(left))
	}
}

func TestListTransactionsMonthFilter(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, uid, "M", 0)
	cat, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})

	mkTx := func(date time.Time, cents int64) {
		_, err := s.CreateTransaction(ctx, uid, Transaction{
			Date: date, AccountID: chk, CategoryID: &cat, OutflowCents: cents,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mkTx(time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC), 1_000)
	mkTx(time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC), 2_000)
	mkTx(time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC), 3_000)
	mkTx(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 4_000)

	rows, err := s.ListTransactions(ctx, uid, TxFilter{Month: "2026-04"})
	if err != nil {
		t.Fatal(err)
	}
	apr := append([]Transaction{}, rows...)
	if len(apr) != 2 {
		t.Fatalf("April rows = %d, want 2", len(apr))
	}

	all, _ := s.ListTransactions(ctx, uid, TxFilter{})
	if len(all) != 4 {
		t.Errorf("no-filter rows = %d, want 4", len(all))
	}

	none, _ := s.ListTransactions(ctx, uid, TxFilter{Month: "2027-01"})
	if len(none) != 0 {
		t.Errorf("empty-month rows = %d, want 0", len(none))
	}
}

func TestPaymentScheduleForCategory(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
	gid, _ := s.CreateGroup(ctx, uid, "Monthly", 0)
	visaPay, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Visa Payment"})

	// Apr 2026: assigned $800, spent $800 (paid).
	_ = s.SetAssigned(ctx, uid, "2026-04", visaPay, 80_000)
	_, _ = s.CreateTransaction(ctx, uid, Transaction{
		Date:      time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		AccountID: chk, CategoryID: &visaPay, OutflowCents: 80_000,
	})

	// May 2026: assigned $1000, not paid yet.
	_ = s.SetAssigned(ctx, uid, "2026-05", visaPay, 100_000)

	// Jun 2026: nothing — should fall back.

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	sched, err := s.PaymentScheduleForCategory(ctx, uid, &visaPay, start, 3, 50_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(sched) != 3 {
		t.Fatalf("len = %d, want 3", len(sched))
	}
	if sched[0] != (MonthPayment{Month: "2026-04", Cents: 80_000, Source: PaymentSpent}) {
		t.Errorf("Apr = %+v, want spent/$800", sched[0])
	}
	if sched[1] != (MonthPayment{Month: "2026-05", Cents: 100_000, Source: PaymentAssigned}) {
		t.Errorf("May = %+v, want assigned/$1000", sched[1])
	}
	if sched[2] != (MonthPayment{Month: "2026-06", Cents: 50_000, Source: PaymentDefault}) {
		t.Errorf("Jun = %+v, want default/$500", sched[2])
	}

	// Nil category → all default.
	sched, _ = s.PaymentScheduleForCategory(ctx, uid, nil, start, 2, 12_345)
	for _, m := range sched {
		if m.Source != PaymentDefault || m.Cents != 12_345 {
			t.Errorf("nil-category month should be default 12345, got %+v", m)
		}
	}
}

func TestSinkingFundCarryover(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
	gid, _ := s.CreateGroup(ctx, uid, "Annual", 0)
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	goal := int64(120_000)
	ins, _ := s.CreateCategory(ctx, uid, Category{
		GroupID: gid, Name: "Insurance", GoalCents: &goal, GoalDueDate: &due,
	})

	// Assign $100 in Jan and Feb 2026; no spending.
	if err := s.SetAssigned(ctx, uid, "2026-01", ins, 10_000); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, "2026-02", ins, 10_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, uid, "2026-02")
	if err != nil {
		t.Fatal(err)
	}
	r := findCategoryRow(t, rows, "Insurance")
	if r.AvailableCents != 20_000 {
		t.Errorf("Feb available = %d, want 20000 (carryover)", r.AvailableCents)
	}
	if r.MonthlyTarget <= 0 {
		t.Errorf("monthly target should be > 0, got %d", r.MonthlyTarget)
	}

	// Spend $5 in Feb to make sure spending counts.
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date:      time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
		AccountID: chk, CategoryID: &ins, OutflowCents: 500,
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.MonthBudget(ctx, uid, "2026-02")
	r = findCategoryRow(t, rows, "Insurance")
	if r.AvailableCents != 19_500 {
		t.Errorf("after Feb spend, available = %d, want 19500", r.AvailableCents)
	}
}

func TestUpdateTransfer(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	sav, _ := s.CreateAccount(ctx, uid, Account{Name: "Sav", Type: TypeChecking})
	wallet, _ := s.CreateAccount(ctx, uid, Account{Name: "Wallet", Type: TypeChecking})
	gid, _ := s.CreateGroup(ctx, uid, "Bills", 0)
	cat, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "CC Payment"})

	// $100 transfer: Chk (out) -> Sav (in), category on the from-leg.
	outID, inID, err := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: sav,
		AmountCents: 10_000, CategoryID: &cat,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	date := time.Now()

	// Rule 2: change the out leg's outflow to $50 -> the in leg's inflow mirrors.
	if err := s.UpdateTransfer(ctx, uid, outID, TransferLegEdit{
		Date: date, AccountID: chk, TransferAccountID: sav,
		OutflowCents: 5_000, CategoryID: &cat,
	}); err != nil {
		t.Fatalf("update amount: %v", err)
	}
	out := getTx(t, s, uid, outID)
	in := getTx(t, s, uid, inID)
	if out.OutflowCents != 5_000 || out.InflowCents != 0 {
		t.Errorf("out leg = (%d,%d), want (5000,0)", out.OutflowCents, out.InflowCents)
	}
	if in.InflowCents != 5_000 || in.OutflowCents != 0 {
		t.Errorf("in leg = (%d,%d), want inflow 5000", in.OutflowCents, in.InflowCents)
	}

	// Rule 2 (direction flip): flip the edited leg from outflow to inflow ->
	// the paired leg flips the opposite way, and the category follows the
	// spending (outflow) leg, which is now the pair.
	if err := s.UpdateTransfer(ctx, uid, outID, TransferLegEdit{
		Date: date, AccountID: chk, TransferAccountID: sav,
		InflowCents: 5_000, CategoryID: &cat,
	}); err != nil {
		t.Fatalf("update flip: %v", err)
	}
	out = getTx(t, s, uid, outID)
	in = getTx(t, s, uid, inID)
	if out.InflowCents != 5_000 || out.OutflowCents != 0 {
		t.Errorf("edited leg after flip = (%d,%d), want inflow 5000", out.OutflowCents, out.InflowCents)
	}
	if in.OutflowCents != 5_000 || in.InflowCents != 0 {
		t.Errorf("paired leg after flip = (%d,%d), want outflow 5000", in.OutflowCents, in.InflowCents)
	}
	if out.CategoryID != nil {
		t.Errorf("inflow leg should be uncategorized, got %v", *out.CategoryID)
	}
	if in.CategoryID == nil || *in.CategoryID != cat {
		t.Errorf("category should follow the outflow (spending) leg")
	}

	// Restore the original direction for the remaining checks.
	mustUpdate(t, s, uid, outID, TransferLegEdit{
		Date: date, AccountID: chk, TransferAccountID: sav,
		OutflowCents: 5_000, CategoryID: &cat,
	})

	// Rule 1: change the edited leg's account only. The paired leg keeps its
	// own posted account, but its back-pointer follows so the label stays truthful.
	mustUpdate(t, s, uid, outID, TransferLegEdit{
		Date: date, AccountID: wallet, TransferAccountID: sav,
		OutflowCents: 5_000, CategoryID: &cat,
	})
	out = getTx(t, s, uid, outID)
	in = getTx(t, s, uid, inID)
	if out.AccountID != wallet {
		t.Errorf("edited leg account = %d, want wallet %d", out.AccountID, wallet)
	}
	if in.AccountID != sav {
		t.Errorf("paired leg account moved to %d, should stay at sav %d", in.AccountID, sav)
	}
	if in.TransferAccountID == nil || *in.TransferAccountID != wallet {
		t.Errorf("paired leg back-pointer = %v, want wallet %d (truthful label)", in.TransferAccountID, wallet)
	}

	// Rule 6: change the edited leg's "transfer to" -> the paired leg moves.
	mustUpdate(t, s, uid, outID, TransferLegEdit{
		Date: date, AccountID: wallet, TransferAccountID: chk,
		OutflowCents: 5_000, CategoryID: &cat,
	})
	out = getTx(t, s, uid, outID)
	in = getTx(t, s, uid, inID)
	if in.AccountID != chk {
		t.Errorf("paired leg should move to chk %d, got %d", chk, in.AccountID)
	}
	if out.TransferAccountID == nil || *out.TransferAccountID != chk {
		t.Errorf("edited leg transfer_account = %v, want chk %d", out.TransferAccountID, chk)
	}

	// Rule 4: date applies to both legs.
	newDate := time.Now().AddDate(0, 0, -3)
	mustUpdate(t, s, uid, outID, TransferLegEdit{
		Date: newDate, AccountID: wallet, TransferAccountID: chk,
		OutflowCents: 5_000, CategoryID: &cat,
	})
	out = getTx(t, s, uid, outID)
	in = getTx(t, s, uid, inID)
	if out.Date.Format("2006-01-02") != newDate.Format("2006-01-02") ||
		in.Date.Format("2006-01-02") != newDate.Format("2006-01-02") {
		t.Errorf("both legs should share date %v: out=%v in=%v", newDate, out.Date, in.Date)
	}

	// Guard: from and to accounts must differ.
	if err := s.UpdateTransfer(ctx, uid, outID, TransferLegEdit{
		Date: date, AccountID: chk, TransferAccountID: chk, OutflowCents: 5_000,
	}); err == nil {
		t.Error("expected error when from == to")
	}

	// Guard: updating a non-transfer row is rejected.
	plain, _ := s.CreateTransaction(ctx, uid, Transaction{Date: date, AccountID: chk, OutflowCents: 100})
	if err := s.UpdateTransfer(ctx, uid, plain, TransferLegEdit{
		Date: date, AccountID: chk, TransferAccountID: sav, OutflowCents: 100,
	}); err == nil {
		t.Error("expected error when leg is not a transfer")
	}
}

// TestUpdateTransferKeepsCleared: reconciling is SetCleared's job. Editing a
// transfer that has already been matched against a statement must not silently
// un-reconcile it — and because the two legs are always cleared together, that
// has to hold for the pair as well as the edited leg.
func TestUpdateTransferKeepsCleared(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	sav, _ := s.CreateAccount(ctx, uid, Account{Name: "Sav", Type: TypeChecking})

	outID, inID, err := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: sav, AmountCents: 10_000,
	})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if err := s.SetCleared(ctx, uid, outID, true); err != nil {
		t.Fatalf("set cleared: %v", err)
	}

	mustUpdate(t, s, uid, outID, TransferLegEdit{
		Date: time.Now(), AccountID: chk, TransferAccountID: sav, OutflowCents: 7_500,
	})

	if out := getTx(t, s, uid, outID); !out.Cleared {
		t.Error("editing un-reconciled the edited leg")
	}
	if in := getTx(t, s, uid, inID); !in.Cleared {
		t.Error("editing un-reconciled the paired leg")
	}

	// The edit itself still has to land.
	if out := getTx(t, s, uid, outID); out.OutflowCents != 7_500 {
		t.Errorf("out leg = %d, want 7500", out.OutflowCents)
	}
}

func TestSetClearedTransfer(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	sav, _ := s.CreateAccount(ctx, uid, Account{Name: "Sav", Type: TypeChecking})

	outID, inID, _ := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: sav, AmountCents: 10_000,
	})

	// Rule 3: toggling cleared on one leg applies to both.
	if err := s.SetCleared(ctx, uid, inID, true); err != nil {
		t.Fatalf("set cleared: %v", err)
	}
	if !getTx(t, s, uid, outID).Cleared || !getTx(t, s, uid, inID).Cleared {
		t.Error("both legs should be cleared")
	}
	if err := s.SetCleared(ctx, uid, outID, false); err != nil {
		t.Fatalf("unset cleared: %v", err)
	}
	if getTx(t, s, uid, outID).Cleared || getTx(t, s, uid, inID).Cleared {
		t.Error("both legs should be uncleared")
	}
}

func getTx(t *testing.T, s *Store, userID, id int64) *Transaction {
	t.Helper()
	tx, err := s.GetTransaction(context.Background(), userID, id)
	if err != nil {
		t.Fatalf("get tx %d: %v", id, err)
	}
	return tx
}

func mustUpdate(t *testing.T, s *Store, userID, legID int64, in TransferLegEdit) {
	t.Helper()
	if err := s.UpdateTransfer(context.Background(), userID, legID, in); err != nil {
		t.Fatalf("update transfer: %v", err)
	}
}

func TestMaxSortOrderHelpers(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	// Groups: the seed migration adds an "Income" group at sort_order -100,
	// so append helpers must key off MAX, not COUNT.
	if _, err := s.CreateGroup(ctx, uid, "Bills", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup(ctx, uid, "Fun", 9); err != nil {
		t.Fatal(err)
	}
	if g, err := s.MaxGroupSortOrder(ctx, uid); err != nil {
		t.Fatalf("max group: %v", err)
	} else if g != 9 {
		t.Errorf("max group sort = %d, want 9", g)
	}
	// MinGroupSortOrder keys off the seed "Income" group at -100, so prepend
	// helpers (min-1) land a new group above every existing group.
	if g, err := s.MinGroupSortOrder(ctx, uid); err != nil {
		t.Fatalf("min group: %v", err)
	} else if g != -100 {
		t.Errorf("min group sort = %d, want -100", g)
	}

	gid, err := s.CreateGroup(ctx, uid, "Home", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Rent", SortOrder: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Power", SortOrder: 7}); err != nil {
		t.Fatal(err)
	}
	if c, err := s.MaxCategorySortOrder(ctx, uid, gid); err != nil {
		t.Fatal(err)
	} else if c != 7 {
		t.Errorf("max cat sort = %d, want 7", c)
	}

	empty, err := s.CreateGroup(ctx, uid, "Empty", 0)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := s.MaxCategorySortOrder(ctx, uid, empty); c != 0 {
		t.Errorf("empty group cat sort = %d, want 0", c)
	}

	month := "2026-07"
	if v, _ := s.MaxIncomeSortOrder(ctx, uid, month); v != 0 {
		t.Errorf("empty income sort = %d, want 0", v)
	}
	if _, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: "Salary", AmountCents: 1000, SortOrder: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: "Side", AmountCents: 500, SortOrder: 8}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.MaxIncomeSortOrder(ctx, uid, month); v != 8 {
		t.Errorf("income sort = %d, want 8", v)
	}
}

func TestReorderGroups(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	a, err := s.CreateGroup(ctx, uid, "Alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateGroup(ctx, uid, "Bravo", 2)
	if err != nil {
		t.Fatal(err)
	}
	cID, err := s.CreateGroup(ctx, uid, "Charlie", 3)
	if err != nil {
		t.Fatal(err)
	}

	// Reorder to Charlie, Alpha, Bravo.
	if err := s.ReorderGroups(ctx, uid, []int64{cID, a, b}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	groups, err := s.ListGroups(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	// The seed "Income" group sits at sort_order -100 and always sorts first;
	// drop it so we compare just the reordered user groups.
	var names []string
	for _, g := range groups {
		if g.Name == "Income" {
			continue
		}
		names = append(names, g.Name)
	}
	if got, want := strings.Join(names, ","), "Charlie,Alpha,Bravo"; got != want {
		t.Errorf("group order = %q, want %q", got, want)
	}
}

func TestReorderCategories(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	g1, _ := s.CreateGroup(ctx, uid, "G1", 1)
	g2, _ := s.CreateGroup(ctx, uid, "G2", 2)
	a, _ := s.CreateCategory(ctx, uid, Category{GroupID: g1, Name: "A", SortOrder: 0})
	b, _ := s.CreateCategory(ctx, uid, Category{GroupID: g1, Name: "B", SortOrder: 1})
	c, _ := s.CreateCategory(ctx, uid, Category{GroupID: g1, Name: "C", SortOrder: 2})

	// Within-group reorder: C, A, B.
	if err := s.ReorderCategories(ctx, uid, g1, []int64{c, a, b}); err != nil {
		t.Fatalf("within-group: %v", err)
	}
	if got := catNamesInGroup(t, s, uid, g1); got != "C,A,B" {
		t.Errorf("g1 order = %q, want %q", got, "C,A,B")
	}

	// Cross-group move: A goes to G2. Destination gains A, source keeps C, B.
	if err := s.ReorderCategories(ctx, uid, g2, []int64{a}); err != nil {
		t.Fatalf("dest: %v", err)
	}
	if err := s.ReorderCategories(ctx, uid, g1, []int64{c, b}); err != nil {
		t.Fatalf("src: %v", err)
	}
	if got := catNamesInGroup(t, s, uid, g1); got != "C,B" {
		t.Errorf("g1 after move = %q, want %q", got, "C,B")
	}
	if got := catNamesInGroup(t, s, uid, g2); got != "A" {
		t.Errorf("g2 after move = %q, want %q", got, "A")
	}

	// A group the user doesn't own is rejected.
	if err := s.ReorderCategories(ctx, uid, 999999, []int64{a}); err == nil {
		t.Error("reorder into foreign group: want error, got nil")
	}
}

func catNamesInGroup(t *testing.T, s *Store, uid, gid int64) string {
	t.Helper()
	cats, err := s.ListCategories(context.Background(), uid, false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cats {
		if c.GroupID == gid && !c.IsIncome {
			names = append(names, c.Name)
		}
	}
	return strings.Join(names, ",")
}

func TestDeleteGroup(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	groupExists := func(id int64) bool {
		groups, err := s.ListGroups(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.ID == id {
				return true
			}
		}
		return false
	}

	// (a) A truly empty group deletes.
	g1, _ := s.CreateGroup(ctx, uid, "Empty", 1)
	if err := s.DeleteGroup(ctx, uid, g1); err != nil {
		t.Fatalf("empty group: %v", err)
	}
	if groupExists(g1) {
		t.Error("empty group should be gone")
	}

	// (b) A group with an active category is rejected.
	g2, _ := s.CreateGroup(ctx, uid, "HasActive", 2)
	if _, err := s.CreateCategory(ctx, uid, Category{GroupID: g2, Name: "Active"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, uid, g2); err == nil {
		t.Error("group with active category should be rejected")
	}
	if !groupExists(g2) {
		t.Error("rejected group should remain")
	}

	// (c) A group whose only categories are archived and unreferenced deletes,
	// cleaning up the archived rows (this is the reported bug).
	g3, _ := s.CreateGroup(ctx, uid, "HasArchived", 3)
	c3, _ := s.CreateCategory(ctx, uid, Category{GroupID: g3, Name: "Old"})
	if err := s.ArchiveCategory(ctx, uid, c3); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, uid, g3); err != nil {
		t.Fatalf("group with unreferenced archived category: %v", err)
	}
	if groupExists(g3) {
		t.Error("group with only archived-unused categories should be gone")
	}

	// (d) A group with an archived category that still carries transaction
	// history is rejected, and left intact.
	acctID, _ := s.CreateAccount(ctx, uid, Account{Name: "Checking", Type: TypeChecking})
	g4, _ := s.CreateGroup(ctx, uid, "HasHistory", 4)
	c4, _ := s.CreateCategory(ctx, uid, Category{GroupID: g4, Name: "Spent"})
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Now(), AccountID: acctID, CategoryID: &c4, OutflowCents: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveCategory(ctx, uid, c4); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, uid, g4); err == nil {
		t.Error("group with referenced archived category should be rejected")
	}
	if !groupExists(g4) {
		t.Error("group with history should remain")
	}
}

func TestCategoryRolloverModeDefaultAndSet(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	gid, _ := s.CreateGroup(ctx, uid, "G", 0)
	id, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})
	if err != nil {
		t.Fatal(err)
	}

	cats, _ := s.ListCategories(ctx, uid, false)
	got := findCat(t, cats, id)
	if got.RolloverMode != RolloverCarryPositive {
		t.Errorf("default rollover_mode = %q, want %q", got.RolloverMode, RolloverCarryPositive)
	}

	if err := s.SetRolloverMode(ctx, uid, id, RolloverNone); err != nil {
		t.Fatal(err)
	}
	cats, _ = s.ListCategories(ctx, uid, false)
	if got := findCat(t, cats, id); got.RolloverMode != RolloverNone {
		t.Errorf("after set, rollover_mode = %q, want %q", got.RolloverMode, RolloverNone)
	}
}

func findCat(t *testing.T, cats []Category, id int64) Category {
	t.Helper()
	for _, c := range cats {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("category id %d not found", id)
	return Category{}
}

func TestRolloverModes(t *testing.T) {
	// Scenario per category: Jan assign $100, spend $150 (overspend $50);
	// Feb assign $0, spend $0. What is Feb available?
	//   carry_positive: Jan overspend resets → Feb available = 0
	//   carry:          Jan −50 carries      → Feb available = -5000
	//   none:           isolated             → Feb available = 0
	cases := []struct {
		mode    string
		wantFeb int64
	}{
		{RolloverCarryPositive, 0},
		{RolloverCarry, -5000},
		{RolloverNone, 0},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			s, uid := newTestStoreUser(t)
			ctx := context.Background()
			chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
			gid, _ := s.CreateGroup(ctx, uid, "G", 0)
			cat, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Fun", RolloverMode: tc.mode})

			if err := s.SetAssigned(ctx, uid, "2026-01", cat, 10_000); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateTransaction(ctx, uid, Transaction{
				Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), AccountID: chk, CategoryID: &cat, OutflowCents: 15_000,
			}); err != nil {
				t.Fatal(err)
			}

			rows, err := s.MonthBudget(ctx, uid, "2026-02")
			if err != nil {
				t.Fatal(err)
			}
			r := findCategoryRow(t, rows, "Fun")
			if r.AvailableCents != tc.wantFeb {
				t.Errorf("mode %s: Feb available = %d, want %d", tc.mode, r.AvailableCents, tc.wantFeb)
			}
		})
	}
}

func TestRolloverNoneIgnoresCarryIn(t *testing.T) {
	// none mode with a prior surplus: Jan leftover must NOT flow into Feb.
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
	gid, _ := s.CreateGroup(ctx, uid, "G", 0)
	cat, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Iso", RolloverMode: RolloverNone})
	_ = chk

	if err := s.SetAssigned(ctx, uid, "2026-01", cat, 10_000); err != nil { // $100 surplus, unspent
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, "2026-02", cat, 3_000); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.MonthBudget(ctx, uid, "2026-02")
	r := findCategoryRow(t, rows, "Iso")
	if r.AvailableCents != 3_000 { // Feb assigned only, no carry-in
		t.Errorf("none-mode Feb available = %d, want 3000", r.AvailableCents)
	}
}

func TestUncategorizedSpent(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	chk, _ := s.CreateAccount(ctx, uid, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
	sav, _ := s.CreateAccount(ctx, uid, Account{Name: "Sav", Type: TypeSavings})
	gid, _ := s.CreateGroup(ctx, uid, "G", 0)
	cat, _ := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Cat"})

	// Uncategorized outflow $40 and inflow $10 in Feb → net 3000.
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), AccountID: chk, OutflowCents: 4_000,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC), AccountID: chk, InflowCents: 1_000,
	}); err != nil {
		t.Fatal(err)
	}
	// Categorized tx must NOT count.
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC), AccountID: chk, CategoryID: &cat, OutflowCents: 9_999,
	}); err != nil {
		t.Fatal(err)
	}
	// A transfer (no category) must NOT count. CreateTransfer returns (fromID, toID, error).
	if _, _, err := s.CreateTransfer(ctx, uid, TransferInput{
		Date: time.Date(2026, 2, 6, 0, 0, 0, 0, time.UTC), FromAccountID: chk, ToAccountID: sav, AmountCents: 5_000,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.UncategorizedSpent(ctx, uid, "2026-02")
	if err != nil {
		t.Fatal(err)
	}
	if got != 3_000 {
		t.Errorf("UncategorizedSpent = %d, want 3000", got)
	}
}
