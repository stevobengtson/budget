package store

import (
	"context"
	"testing"
	"time"
)

// TestNeedsReviewFlow covers the review lifecycle around bank-sync imports:
// the filter, the count, mark-one, mark-all scoping, and the categorize-
// clears-the-flag rule in UpdateTransaction.
func TestNeedsReviewFlow(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	acctID, err := s.CreateAccount(ctx, uid, Account{Name: "Chequing", Type: TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	otherAcct, _ := s.CreateAccount(ctx, uid, Account{Name: "Other", Type: TypeChecking})
	itemID, err := s.CreatePlaidItem(ctx, PlaidItem{UserID: uid, PlaidItemID: "item-r", AccessTokenEncrypted: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkAccountToPlaid(ctx, uid, acctID, itemID, "pa-r", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	// Three flagged imports (two in acctID, one in otherAcct) + one manual row.
	batch := PlaidSyncBatch{
		Upserts: []PlaidTxUpsert{
			{PlaidTransactionID: "r1", AccountID: acctID, Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Payee: "A", OutflowCents: 100, Cleared: true, NeedsReview: true},
			{PlaidTransactionID: "r2", AccountID: acctID, Date: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Payee: "B", OutflowCents: 200, Cleared: true, NeedsReview: true},
			{PlaidTransactionID: "r3", AccountID: otherAcct, Date: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), Payee: "C", OutflowCents: 300, Cleared: true, NeedsReview: true},
		},
		NextCursor: "c1",
	}
	if err := s.ApplyPlaidSync(ctx, uid, itemID, batch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTransaction(ctx, uid, Transaction{Date: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC), AccountID: acctID, OutflowCents: 50}); err != nil {
		t.Fatal(err)
	}

	if n, _ := s.CountNeedsReview(ctx, uid); n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	flagged, err := s.ListTransactions(ctx, uid, TxFilter{NeedsReview: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(flagged) != 3 {
		t.Fatalf("filtered rows = %d, want 3", len(flagged))
	}

	// Categorizing clears the flag; an uncategorized edit keeps it.
	cat := seedTestCategory(t, s, uid)
	r1 := findByPlaidID(t, flagged, "r1")
	r1.CategoryID = &cat
	if err := s.UpdateTransaction(ctx, uid, r1); err != nil {
		t.Fatal(err)
	}
	r2 := findByPlaidID(t, flagged, "r2")
	notes := "looked at it, still thinking"
	r2.Notes = &notes
	if err := s.UpdateTransaction(ctx, uid, r2); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountNeedsReview(ctx, uid); n != 2 {
		t.Fatalf("count after categorize = %d, want 2 (r1 cleared, r2 kept)", n)
	}

	// Mark one reviewed.
	if err := s.SetTransactionReviewed(ctx, uid, r2.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountNeedsReview(ctx, uid); n != 1 {
		t.Fatalf("count after mark-one = %d, want 1", n)
	}

	// Mark-all scoped to acctID must not touch otherAcct's row.
	if n, err := s.MarkAllReviewed(ctx, uid, TxFilter{AccountID: &acctID}); err != nil || n != 0 {
		t.Fatalf("mark-all(acct) = %d, %v — acct rows were already reviewed", n, err)
	}
	if n, err := s.MarkAllReviewed(ctx, uid, TxFilter{}); err != nil || n != 1 {
		t.Fatalf("mark-all(everything) = %d, %v, want 1", n, err)
	}
	if n, _ := s.CountNeedsReview(ctx, uid); n != 0 {
		t.Fatalf("final count = %d, want 0", n)
	}
}

func findByPlaidID(t *testing.T, txs []Transaction, id string) Transaction {
	t.Helper()
	for _, tx := range txs {
		if tx.PlaidTransactionID != nil && *tx.PlaidTransactionID == id {
			return tx
		}
	}
	t.Fatalf("no transaction with plaid id %s", id)
	return Transaction{}
}

func seedTestCategory(t *testing.T, s *Store, uid int64) int64 {
	t.Helper()
	ctx := context.Background()
	gid, err := s.CreateGroup(ctx, uid, "Review Group", 0)
	if err != nil {
		t.Fatal(err)
	}
	cid, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Review Cat"})
	if err != nil {
		t.Fatal(err)
	}
	return cid
}
