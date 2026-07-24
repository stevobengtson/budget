package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// countUserRows returns the number of rows a user owns in one of the budget
// tables. table is a fixed test literal, never user input.
func countUserRows(t *testing.T, s *Store, table string, userID int64) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// seedBudgetData gives a user one of everything: an account, a group + category,
// a transaction, an income row, and a budget assignment.
func seedBudgetData(t *testing.T, s *Store, uid int64) {
	t.Helper()
	ctx := context.Background()
	acct, err := s.CreateAccount(ctx, uid, Account{Name: "Checking", Type: TypeChecking, StartingBalanceCents: 100_000})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	gid, err := s.CreateGroup(ctx, uid, "Monthly", 0)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	cat, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Groceries"})
	if err != nil {
		t.Fatalf("category: %v", err)
	}
	// A credit account whose payment_category_id points at a category exercises
	// the accounts→categories foreign key, so the delete order must remove
	// accounts before categories.
	if _, err := s.CreateAccount(ctx, uid, Account{
		Name: "Visa", Type: TypeCredit, PaymentCategoryID: &cat,
	}); err != nil {
		t.Fatalf("credit account: %v", err)
	}
	month := "2026-04"
	if _, err := s.CreateTransaction(ctx, uid, Transaction{
		Date: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), AccountID: acct, CategoryID: &cat, OutflowCents: 5_000,
	}); err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if _, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: "Salary", AmountCents: 300_000}); err != nil {
		t.Fatalf("income: %v", err)
	}
	if err := s.SetAssigned(ctx, uid, month, cat, 20_000); err != nil {
		t.Fatalf("assign: %v", err)
	}
}

var budgetTables = []string{"accounts", "category_groups", "categories", "transactions", "incomes", "budgets"}

// TestWipeUserData removes only the target user's budget content, leaving a
// second user's data untouched.
func TestWipeUserData(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	// A second user with their own data acts as a canary for over-deletion.
	other, err := s.CreateUser(ctx, "other@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	seedBudgetData(t, s, other)

	seedBudgetData(t, s, uid)
	for _, tbl := range budgetTables {
		if countUserRows(t, s, tbl, uid) == 0 {
			t.Fatalf("precondition: user has no %s rows", tbl)
		}
	}

	if err := s.WipeUserData(ctx, uid); err != nil {
		t.Fatalf("WipeUserData: %v", err)
	}

	for _, tbl := range budgetTables {
		if n := countUserRows(t, s, tbl, uid); n != 0 {
			t.Errorf("after wipe, %s has %d rows, want 0", tbl, n)
		}
		if n := countUserRows(t, s, tbl, other); n == 0 {
			t.Errorf("wipe deleted the other user's %s rows", tbl)
		}
	}

	// The account itself remains; a wiped user can start fresh.
	if _, err := s.GetUserByID(ctx, uid); err != nil {
		t.Errorf("wiped user should still exist: %v", err)
	}
}

// TestDeleteUser removes the user, all their budget data, and cascades the
// auth/billing rows (sessions, add-on links, subscriptions), without touching a
// second user.
func TestDeleteUser(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	other, err := s.CreateUser(ctx, "other@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	seedBudgetData(t, s, other)

	seedBudgetData(t, s, uid)
	if err := s.CreateSession(ctx, uid, "tokenhash", time.Now().Add(time.Hour), "ua", "ip"); err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := s.SetAddOnEnabled(ctx, uid, "paydown", true); err != nil {
		t.Fatalf("add-on: %v", err)
	}
	if err := s.UpsertSubscription(ctx, Subscription{
		UserID: uid, StripeSubscriptionID: "sub_test", StripeCustomerID: "cus_test",
		PriceID: "price_test", Status: "active",
	}); err != nil {
		t.Fatalf("subscription: %v", err)
	}

	if err := s.DeleteUser(ctx, uid); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// The user is gone.
	if _, err := s.GetUserByID(ctx, uid); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetUserByID after delete = %v, want sql.ErrNoRows", err)
	}
	// Budget data is gone.
	for _, tbl := range budgetTables {
		if n := countUserRows(t, s, tbl, uid); n != 0 {
			t.Errorf("after delete, %s has %d rows, want 0", tbl, n)
		}
	}
	// Cascaded rows are gone.
	if _, err := s.GetSessionByTokenHash(ctx, "tokenhash"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("session after delete = %v, want sql.ErrNoRows", err)
	}
	for _, tbl := range []string{"user_add_ons", "subscriptions", "sessions"} {
		if n := countUserRows(t, s, tbl, uid); n != 0 {
			t.Errorf("after delete, %s has %d rows, want 0", tbl, n)
		}
	}

	// The other user is untouched.
	if _, err := s.GetUserByID(ctx, other); err != nil {
		t.Errorf("other user should still exist: %v", err)
	}
	if n := countUserRows(t, s, "accounts", other); n == 0 {
		t.Errorf("delete removed the other user's accounts")
	}
}
