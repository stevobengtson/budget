package store

import (
	"context"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	conn, _, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return New(conn)
}

func TestAccountsCRUDAndBalance(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateAccount(ctx, Account{
		Name: "Checking", Type: TypeChecking, StartingBalanceCents: 100_000,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
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
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	visa, _ := s.CreateAccount(ctx, Account{Name: "Visa", Type: TypeCredit})
	gid, _ := s.CreateGroup(ctx, "Monthly", 0)
	groc, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Groceries"})

	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: time.Now(), AccountID: chk, CategoryID: &groc, OutflowCents: 8000,
	}); err != nil {
		t.Fatalf("tx1: %v", err)
	}
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: time.Now(), AccountID: visa, CategoryID: &groc, OutflowCents: 5000,
	}); err != nil {
		t.Fatalf("tx2: %v", err)
	}

	accs, _ := s.ListAccounts(ctx, false)
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
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	visa, _ := s.CreateAccount(ctx, Account{Name: "Visa", Type: TypeCredit})

	out, in, err := s.CreateTransfer(ctx, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: visa, AmountCents: 10_000,
	})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if out == 0 || in == 0 {
		t.Fatalf("transfer ids missing")
	}

	accs, _ := s.ListAccounts(ctx, false)
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
	if err := s.DeleteTransaction(ctx, out); err != nil {
		t.Fatalf("delete: %v", err)
	}
	txs, _ := s.ListTransactions(ctx, TxFilter{})
	if len(txs) != 0 {
		t.Errorf("expected 0 txs after transfer delete, got %d", len(txs))
	}
}

func TestMonthBudget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, "Monthly", 0)
	groc, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Groceries"})

	now := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// Spend $80 this month.
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: chk, CategoryID: &groc, OutflowCents: 8000,
	}); err != nil {
		t.Fatal(err)
	}
	// Assign $200 this month.
	if err := s.SetAssigned(ctx, month, groc, 20_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, month)
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

func TestMonthBudgetSpentNetsInflows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, "Monthly", 0)
	dining, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Dining Out"})

	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// Spend $80, then receive a $45 refund into the same category.
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: chk, CategoryID: &dining, OutflowCents: 8000,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: chk, CategoryID: &dining, InflowCents: 4500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, month, dining, 20_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, month)
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
	s := newTestStore(t)
	ctx := context.Background()

	month := "2026-04"
	work, err := s.CreateIncome(ctx, Income{Month: month, Name: "Work", AmountCents: 980_000})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	if _, err := s.CreateIncome(ctx, Income{Month: month, Name: "Government", AmountCents: 6_600}); err != nil {
		t.Fatalf("create gov: %v", err)
	}
	if _, err := s.CreateIncome(ctx, Income{Month: month, Name: "Contract", AmountCents: 80_000}); err != nil {
		t.Fatalf("create contract: %v", err)
	}

	total, err := s.TotalIncome(ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1_066_600 {
		t.Errorf("total = %d, want 1066600", total)
	}

	rows, err := s.ListIncomes(ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}

	// Different month is isolated.
	if _, err := s.CreateIncome(ctx, Income{Month: "2026-05", Name: "Work", AmountCents: 1}); err != nil {
		t.Fatal(err)
	}
	if total, _ := s.TotalIncome(ctx, month); total != 1_066_600 {
		t.Errorf("total leaked across months: %d", total)
	}

	// Update + delete.
	if err := s.UpdateIncome(ctx, Income{ID: work, Name: "Work", AmountCents: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	if total, _ := s.TotalIncome(ctx, month); total != 1_086_600 {
		t.Errorf("after update, total = %d, want 1086600", total)
	}
	if err := s.DeleteIncome(ctx, work); err != nil {
		t.Fatal(err)
	}
	if total, _ := s.TotalIncome(ctx, month); total != 86_600 {
		t.Errorf("after delete, total = %d, want 86600", total)
	}
}

func TestIncomeCategorySeededAndLocked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cats, err := s.ListCategories(ctx, false)
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

	if err := s.ArchiveCategory(ctx, income.ID); err == nil {
		t.Error("ArchiveCategory should refuse income category")
	}
	if err := s.DeleteCategory(ctx, income.ID); err == nil {
		t.Error("DeleteCategory should refuse income category")
	}
}

func TestActualIncomeForMonth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking})

	// Find seeded Income.
	cats, _ := s.ListCategories(ctx, false)
	var income int64
	for _, c := range cats {
		if c.IsIncome {
			income = c.ID
		}
	}

	now := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// Paycheck inflow categorized as Income.
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: chk, CategoryID: &income, InflowCents: 500_000,
	}); err != nil {
		t.Fatal(err)
	}
	// Refund inflow on a non-income category — must NOT count.
	gid, _ := s.CreateGroup(ctx, "Misc", 0)
	other, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Refunds"})
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: chk, CategoryID: &other, InflowCents: 1_000,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ActualIncomeForMonth(ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500_000 {
		t.Errorf("actual income = %d, want 500000", got)
	}
}

func TestCreditCardActivityForMonth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000_00})
	visa, _ := s.CreateAccount(ctx, Account{Name: "Visa", Type: TypeCredit, StartingBalanceCents: -100_000})
	gid, _ := s.CreateGroup(ctx, "Bills", 0)
	groc, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Groceries"})

	now := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)
	month := MonthKey(now)

	// $500 of purchases on Visa.
	_, _ = s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: visa, CategoryID: &groc, OutflowCents: 50_000,
	})
	// Bank-booked interest on Visa (no category) — should also count as
	// "purchase" since it raises the balance owed.
	_, _ = s.CreateTransaction(ctx, Transaction{
		Date: now, AccountID: visa, OutflowCents: 4_500,
	})
	// $300 transfer from Checking → Visa (covers some of the spend).
	_, _, _ = s.CreateTransfer(ctx, TransferInput{
		Date: now, FromAccountID: chk, ToAccountID: visa, AmountCents: 30_000,
	})

	// Activity from a different month must not leak.
	other := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	_, _ = s.CreateTransaction(ctx, Transaction{
		Date: other, AccountID: visa, OutflowCents: 99_999,
	})

	rows, err := s.CreditCardActivityForMonth(ctx, month)
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
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 200_000})
	limit := int64(500_000)
	apr := int64(2099)
	visa, _ := s.CreateAccount(ctx, Account{
		Name: "Visa", Type: TypeCredit, CreditLimitCents: &limit, AprBps: &apr,
		StartingBalanceCents: -100_000, // owe $1000
	})
	gid, _ := s.CreateGroup(ctx, "Bills", 0)
	ccPay, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "CC Payment"})
	if err := s.SetAssigned(ctx, MonthKey(time.Now()), ccPay, 10_000); err != nil {
		t.Fatal(err)
	}

	// Interest charge on the card itself (no category — purely a balance event).
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date: time.Now(), AccountID: visa, OutflowCents: 4_500,
	}); err != nil {
		t.Fatal(err)
	}

	// Transfer 1: cover interest.
	if _, _, err := s.CreateTransfer(ctx, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: visa,
		AmountCents: 4_500, CategoryID: &ccPay,
	}); err != nil {
		t.Fatal(err)
	}
	// Transfer 2: pay down principal.
	if _, _, err := s.CreateTransfer(ctx, TransferInput{
		Date: time.Now(), FromAccountID: chk, ToAccountID: visa,
		AmountCents: 5_500, CategoryID: &ccPay,
	}); err != nil {
		t.Fatal(err)
	}

	// Visa balance: -100000 - 4500 (interest) + 4500 + 5500 = -94500.
	accs, _ := s.ListAccounts(ctx, false)
	for _, a := range accs {
		if a.ID == visa && a.BalanceCents != -94_500 {
			t.Errorf("Visa balance = %d, want -94500", a.BalanceCents)
		}
		if a.ID == chk && a.BalanceCents != 200_000-4_500-5_500 {
			t.Errorf("Checking balance = %d, want %d", a.BalanceCents, 200_000-10_000)
		}
	}

	// Budget impact: CC Payment should be spent 10000 (4500 + 5500), available 0.
	rows, err := s.MonthBudget(ctx, MonthKey(time.Now()))
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
	txs, _ := s.ListTransactions(ctx, TxFilter{})
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
	if err := s.DeleteTransaction(ctx, firstTransferID); err != nil {
		t.Fatal(err)
	}
	left, _ := s.ListTransactions(ctx, TxFilter{})
	// 1 interest charge + 2 legs of remaining transfer = 3 rows.
	if len(left) != 3 {
		t.Errorf("after delete, %d rows remain, want 3", len(left))
	}
}

func TestListTransactionsMonthFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 100_000})
	gid, _ := s.CreateGroup(ctx, "M", 0)
	cat, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Groceries"})

	mkTx := func(date time.Time, cents int64) {
		_, err := s.CreateTransaction(ctx, Transaction{
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

	rows, err := s.ListTransactions(ctx, TxFilter{Month: "2026-04"})
	if err != nil {
		t.Fatal(err)
	}
	apr := append([]Transaction{}, rows...)
	if len(apr) != 2 {
		t.Fatalf("April rows = %d, want 2", len(apr))
	}

	all, _ := s.ListTransactions(ctx, TxFilter{})
	if len(all) != 4 {
		t.Errorf("no-filter rows = %d, want 4", len(all))
	}

	none, _ := s.ListTransactions(ctx, TxFilter{Month: "2027-01"})
	if len(none) != 0 {
		t.Errorf("empty-month rows = %d, want 0", len(none))
	}
}

func TestPaymentScheduleForCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
	gid, _ := s.CreateGroup(ctx, "Monthly", 0)
	visaPay, _ := s.CreateCategory(ctx, Category{GroupID: gid, Name: "Visa Payment"})

	// Apr 2026: assigned $800, spent $800 (paid).
	_ = s.SetAssigned(ctx, "2026-04", visaPay, 80_000)
	_, _ = s.CreateTransaction(ctx, Transaction{
		Date:      time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		AccountID: chk, CategoryID: &visaPay, OutflowCents: 80_000,
	})

	// May 2026: assigned $1000, not paid yet.
	_ = s.SetAssigned(ctx, "2026-05", visaPay, 100_000)

	// Jun 2026: nothing — should fall back.

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	sched, err := s.PaymentScheduleForCategory(ctx, &visaPay, start, 3, 50_000)
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
	sched, _ = s.PaymentScheduleForCategory(ctx, nil, start, 2, 12_345)
	for _, m := range sched {
		if m.Source != PaymentDefault || m.Cents != 12_345 {
			t.Errorf("nil-category month should be default 12345, got %+v", m)
		}
	}
}

func TestSinkingFundCarryover(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	chk, _ := s.CreateAccount(ctx, Account{Name: "Chk", Type: TypeChecking, StartingBalanceCents: 1_000_000})
	gid, _ := s.CreateGroup(ctx, "Annual", 0)
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	goal := int64(120_000)
	ins, _ := s.CreateCategory(ctx, Category{
		GroupID: gid, Name: "Insurance", GoalCents: &goal, GoalDueDate: &due,
	})

	// Assign $100 in Jan and Feb 2026; no spending.
	if err := s.SetAssigned(ctx, "2026-01", ins, 10_000); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, "2026-02", ins, 10_000); err != nil {
		t.Fatal(err)
	}

	rows, err := s.MonthBudget(ctx, "2026-02")
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
	if _, err := s.CreateTransaction(ctx, Transaction{
		Date:      time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
		AccountID: chk, CategoryID: &ins, OutflowCents: 500,
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.MonthBudget(ctx, "2026-02")
	r = findCategoryRow(t, rows, "Insurance")
	if r.AvailableCents != 19_500 {
		t.Errorf("after Feb spend, available = %d, want 19500", r.AvailableCents)
	}
}
