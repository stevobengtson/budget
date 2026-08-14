package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// countIncomeCats returns how many of a user's categories are the system Income
// category.
func countIncomeCats(t *testing.T, s *Store, userID int64) int {
	t.Helper()
	cats, err := s.ListCategories(context.Background(), userID, false)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range cats {
		if c.IsIncome {
			n++
		}
	}
	return n
}

func TestSeedNewUserCreatesDefaults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "fresh@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SeedNewUser(ctx, uid, i18n.EnCA, nil); err != nil {
		t.Fatal(err)
	}

	groups, err := s.ListGroups(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1+len(StarterGroups) { // Income group + defaults
		t.Fatalf("groups = %d, want %d", len(groups), 1+len(StarterGroups))
	}

	cats, err := s.ListCategories(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	want := 1 // Income
	for _, g := range StarterGroups {
		want += len(g.Categories)
	}
	if len(cats) != want {
		t.Fatalf("categories = %d, want %d", len(cats), want)
	}
	has := func(name string) bool {
		for _, c := range cats {
			if c.Name == name {
				return true
			}
		}
		return false
	}
	for _, n := range []string{"Income", "Groceries", "Emergency Fund"} {
		if !has(n) {
			t.Errorf("missing default category %q", n)
		}
	}
	if got := countIncomeCats(t, s, uid); got != 1 {
		t.Fatalf("income cats = %d, want 1", got)
	}
}

func TestSeedNewUserIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "fresh@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SeedNewUser(ctx, uid, i18n.EnCA, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ListCategories(ctx, uid, false)
	// Second call must be a no-op — no duplicated Income or default categories.
	if err := s.SeedNewUser(ctx, uid, i18n.EnCA, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ListCategories(ctx, uid, false)
	if len(before) != len(after) {
		t.Fatalf("double seed changed category count: %d -> %d", len(before), len(after))
	}
	if got := countIncomeCats(t, s, uid); got != 1 {
		t.Fatalf("income cats after double seed = %d, want 1", got)
	}
}

func TestSeedNewUserSkipsWhenBudgetExists(t *testing.T) {
	// A user who already has income + at least one expense category (e.g. an owner
	// who claimed a real pre-existing budget) must not get the default set added.
	s, uid := newTestStoreUser(t) // owns the claimed global Income
	ctx := context.Background()
	gid, err := s.CreateGroup(ctx, uid, "MyGroup", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "MyCat"}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ListCategories(ctx, uid, false)

	if err := s.SeedNewUser(ctx, uid, i18n.EnCA, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ListCategories(ctx, uid, false)
	if len(before) != len(after) {
		t.Fatalf("seed added categories to a user with an existing budget: %d -> %d", len(before), len(after))
	}
}

func TestUserCRUD(t *testing.T) {
	s := newTestStore(t) // existing helper in store_test.go
	ctx := context.Background()

	id, err := s.CreateUser(ctx, "u@example.com", "hash1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("CountUsers=%d want 1", n)
	}

	u, err := s.GetUserByEmail(ctx, "u@example.com")
	if err != nil || u.ID != id || u.PasswordHash != "hash1" {
		t.Fatalf("get by email: %+v err=%v", u, err)
	}
	if u.EmailVerifiedAt != nil {
		t.Fatal("new user should be unverified")
	}

	if err := s.SetEmailVerified(ctx, id); err != nil {
		t.Fatal(err)
	}
	u, _ = s.GetUserByID(ctx, id)
	if u.EmailVerifiedAt == nil {
		t.Fatal("should be verified now")
	}

	if err := s.UpdatePasswordHash(ctx, id, "hash2"); err != nil {
		t.Fatal(err)
	}
	u, _ = s.GetUserByID(ctx, id)
	if u.PasswordHash != "hash2" {
		t.Fatalf("hash not updated: %q", u.PasswordHash)
	}
	_ = time.Now
}

func TestSessionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, _ := s.CreateUser(ctx, "s@example.com", "h")

	exp := time.Now().Add(time.Hour)
	if err := s.CreateSession(ctx, uid, "hash-a", exp, SessionInfo{UserAgent: "agent", IP: "127.0.0.1"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	sess, err := s.GetSessionByTokenHash(ctx, "hash-a")
	if err != nil || sess.UserID != uid {
		t.Fatalf("get: %+v err=%v", sess, err)
	}
	if err := s.DeleteSession(ctx, "hash-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSessionByTokenHash(ctx, "hash-a"); err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestVerificationTokenSingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, _ := s.CreateUser(ctx, "v@example.com", "h")

	exp := time.Now().Add(time.Hour)
	if err := s.CreateVerificationToken(ctx, uid, "th", "email_verify", exp); err != nil {
		t.Fatal(err)
	}
	got, err := s.ConsumeToken(ctx, "th", "email_verify")
	if err != nil || got != uid {
		t.Fatalf("consume: got=%d err=%v", got, err)
	}
	// Second use must fail (already consumed).
	if _, err := s.ConsumeToken(ctx, "th", "email_verify"); err == nil {
		t.Fatal("expected error on re-consume")
	}
}

func TestVerificationTokenExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, _ := s.CreateUser(ctx, "e@example.com", "h")
	past := time.Now().Add(-time.Hour)
	_ = s.CreateVerificationToken(ctx, uid, "old", "password_reset", past)
	if _, err := s.ConsumeToken(ctx, "old", "password_reset"); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestVerificationTokenWrongPurpose(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, _ := s.CreateUser(ctx, "w@example.com", "h")
	_ = s.CreateVerificationToken(ctx, uid, "p", "email_verify", time.Now().Add(time.Hour))
	if _, err := s.ConsumeToken(ctx, "p", "password_reset"); err == nil {
		t.Fatal("purpose mismatch must fail")
	}
}

// TestCrossUserDataIsolation proves the user-scoped store methods never surface
// one user's data to another. User A owns a full slice of data; user B must see
// none of it through any list/read path, and cannot fetch A's rows by id.
func TestCrossUserDataIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.CreateUser(ctx, "a@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser(ctx, "b@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}

	// User A builds one of everything.
	gid, err := s.CreateGroup(ctx, a, "Bills", 0)
	if err != nil {
		t.Fatal(err)
	}
	catID, err := s.CreateCategory(ctx, a, Category{GroupID: gid, Name: "Rent"})
	if err != nil {
		t.Fatal(err)
	}
	acctID, err := s.CreateAccount(ctx, a, Account{Name: "Checking", Type: TypeChecking, StartingBalanceCents: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTransaction(ctx, a, Transaction{
		Date: time.Now(), AccountID: acctID, CategoryID: &catID, OutflowCents: 2_500,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateIncome(ctx, a, Income{Month: "2026-07", Name: "Salary", AmountCents: 500_000}); err != nil {
		t.Fatal(err)
	}

	// A sees its own rows.
	if accs, _ := s.ListAccounts(ctx, a, true); len(accs) != 1 {
		t.Errorf("A ListAccounts = %d, want 1", len(accs))
	}
	if cats, _ := s.ListCategories(ctx, a, true); len(cats) != 1 {
		t.Errorf("A ListCategories = %d, want 1", len(cats))
	}
	if grps, _ := s.ListGroups(ctx, a); len(grps) != 1 {
		t.Errorf("A ListGroups = %d, want 1", len(grps))
	}
	if incs, _ := s.ListIncomes(ctx, a, "2026-07"); len(incs) != 1 {
		t.Errorf("A ListIncomes = %d, want 1", len(incs))
	}
	if txs, _ := s.ListTransactions(ctx, a, TxFilter{}); len(txs) != 1 {
		t.Errorf("A ListTransactions = %d, want 1", len(txs))
	}

	// B sees NONE of A's rows through any list path.
	if accs, _ := s.ListAccounts(ctx, b, true); len(accs) != 0 {
		t.Errorf("B ListAccounts = %d, want 0 (cross-user leak)", len(accs))
	}
	if cats, _ := s.ListCategories(ctx, b, true); len(cats) != 0 {
		t.Errorf("B ListCategories = %d, want 0 (cross-user leak)", len(cats))
	}
	if grps, _ := s.ListGroups(ctx, b); len(grps) != 0 {
		t.Errorf("B ListGroups = %d, want 0 (cross-user leak)", len(grps))
	}
	if incs, _ := s.ListIncomes(ctx, b, "2026-07"); len(incs) != 0 {
		t.Errorf("B ListIncomes = %d, want 0 (cross-user leak)", len(incs))
	}
	if txs, _ := s.ListTransactions(ctx, b, TxFilter{}); len(txs) != 0 {
		t.Errorf("B ListTransactions = %d, want 0 (cross-user leak)", len(txs))
	}
	if got, _ := s.TotalIncome(ctx, b, "2026-07"); got != 0 {
		t.Errorf("B TotalIncome = %d, want 0 (cross-user leak)", got)
	}
	if rows, _ := s.MonthBudget(ctx, b, "2026-07"); len(rows) != 0 {
		t.Errorf("B MonthBudget = %d rows, want 0 (cross-user leak)", len(rows))
	}

	// B cannot fetch A's rows directly by id.
	if _, err := s.GetAccount(ctx, b, acctID); err == nil {
		t.Error("B GetAccount(A's account id) succeeded; want no-row error")
	}
	aTxs, _ := s.ListTransactions(ctx, a, TxFilter{})
	if _, err := s.GetTransaction(ctx, b, aTxs[0].ID); err == nil {
		t.Error("B GetTransaction(A's tx id) succeeded; want no-row error")
	}
}

// TestCrossUserWriteIsolation proves a user cannot mutate through a foreign-key
// reference to another user's row: every write that takes a user-supplied
// account/category/group id rejects a cross-tenant id with ErrNotOwned and
// leaves the victim's data untouched.
func TestCrossUserWriteIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.CreateUser(ctx, "a@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser(ctx, "b@example.com", "h")
	if err != nil {
		t.Fatal(err)
	}

	// B owns a group, a category (with an assignment), and an account.
	bGroup, err := s.CreateGroup(ctx, b, "B Bills", 0)
	if err != nil {
		t.Fatal(err)
	}
	bCat, err := s.CreateCategory(ctx, b, Category{GroupID: bGroup, Name: "B Rent"})
	if err != nil {
		t.Fatal(err)
	}
	bAcct, err := s.CreateAccount(ctx, b, Account{Name: "B Checking", Type: TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, b, "2026-07", bCat, 5_000); err != nil {
		t.Fatal(err)
	}

	// A owns one account, used where the attack must reference A's own account
	// but B's category.
	aAcct, err := s.CreateAccount(ctx, a, Account{Name: "A Checking", Type: TypeChecking})
	if err != nil {
		t.Fatal(err)
	}

	// SetAssigned on B's category is rejected and does not change B's value.
	if err := s.SetAssigned(ctx, a, "2026-07", bCat, 9_999); !errors.Is(err, ErrNotOwned) {
		t.Errorf("A SetAssigned(B's category) err = %v, want ErrNotOwned", err)
	}
	if got, _ := s.GetAssigned(ctx, b, "2026-07", bCat); got != 5_000 {
		t.Errorf("B assigned after A's blocked write = %d, want 5000 (unchanged)", got)
	}

	// CreateTransaction referencing B's account is rejected.
	if _, err := s.CreateTransaction(ctx, a, Transaction{
		Date: time.Now(), AccountID: bAcct, OutflowCents: 100,
	}); !errors.Is(err, ErrNotOwned) {
		t.Errorf("A CreateTransaction(B's account) err = %v, want ErrNotOwned", err)
	}
	// CreateTransaction on A's own account but referencing B's category is rejected.
	if _, err := s.CreateTransaction(ctx, a, Transaction{
		Date: time.Now(), AccountID: aAcct, CategoryID: &bCat, OutflowCents: 100,
	}); !errors.Is(err, ErrNotOwned) {
		t.Errorf("A CreateTransaction(B's category) err = %v, want ErrNotOwned", err)
	}

	// CreateCategory into B's group is rejected.
	if _, err := s.CreateCategory(ctx, a, Category{GroupID: bGroup, Name: "sneaky"}); !errors.Is(err, ErrNotOwned) {
		t.Errorf("A CreateCategory(B's group) err = %v, want ErrNotOwned", err)
	}

	// CreateAccount with payment_category_id pointing at B's category is rejected.
	if _, err := s.CreateAccount(ctx, a, Account{
		Name: "A Card", Type: TypeCredit, PaymentCategoryID: &bCat,
	}); !errors.Is(err, ErrNotOwned) {
		t.Errorf("A CreateAccount(payment_category=B's category) err = %v, want ErrNotOwned", err)
	}

	// Sanity: B still has exactly its own rows; A leaked nothing into B's view.
	if txs, _ := s.ListTransactions(ctx, b, TxFilter{}); len(txs) != 0 {
		t.Errorf("B ListTransactions = %d, want 0 after blocked writes", len(txs))
	}
	if cats, _ := s.ListCategories(ctx, b, true); len(cats) != 1 {
		t.Errorf("B ListCategories = %d, want 1 (only B Rent)", len(cats))
	}
}
