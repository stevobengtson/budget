package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestPlaidItemCRUDAndLinking(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	itemID, err := s.CreatePlaidItem(ctx, PlaidItem{
		UserID: uid, PlaidItemID: "item-abc",
		AccessTokenEncrypted: []byte{1, 2, 3},
		InstitutionID:        "ins_1", InstitutionName: "Test Bank",
	})
	if err != nil {
		t.Fatal(err)
	}

	it, err := s.GetPlaidItem(ctx, uid, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != PlaidItemActive || it.SyncCursor != "" || string(it.AccessTokenEncrypted) != "\x01\x02\x03" {
		t.Errorf("fresh item wrong: %+v", it)
	}
	if _, err := s.GetPlaidItemByItemID(ctx, "item-abc"); err != nil {
		t.Fatalf("lookup by plaid item id: %v", err)
	}
	// Other users can't see it.
	if _, err := s.GetPlaidItem(ctx, uid+999, itemID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("cross-user get err = %v, want sql.ErrNoRows", err)
	}

	// Link an account, count, list, unlink.
	acctID, err := s.CreateAccount(ctx, uid, Account{Name: "Chequing", Type: TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	syncFrom := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := s.LinkAccountToPlaid(ctx, uid, acctID, itemID, "plaid-acct-1", syncFrom); err != nil {
		t.Fatal(err)
	}
	a, err := s.GetAccount(ctx, uid, acctID)
	if err != nil {
		t.Fatal(err)
	}
	if !a.PlaidLinked() || *a.PlaidAccountID != "plaid-acct-1" || *a.PlaidItemID != itemID {
		t.Errorf("linked account wrong: %+v", a)
	}
	if a.PlaidSyncFrom == nil || !a.PlaidSyncFrom.Equal(syncFrom) {
		t.Errorf("sync from = %v, want %v", a.PlaidSyncFrom, syncFrom)
	}
	if n, _ := s.CountAccountsForPlaidItem(ctx, uid, itemID); n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
	if linked, _ := s.ListLinkedAccounts(ctx, itemID); len(linked) != 1 || linked[0].ID != acctID {
		t.Errorf("linked accounts = %+v", linked)
	}

	// Linking to someone else's item must fail.
	if err := s.LinkAccountToPlaid(ctx, uid+999, acctID, itemID, "x", syncFrom); !errors.Is(err, ErrNotOwned) {
		t.Errorf("cross-user link err = %v, want ErrNotOwned", err)
	}

	// Cursor + status updates.
	if err := s.UpdatePlaidItemCursor(ctx, itemID, "cursor-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlaidItemStatus(ctx, itemID, PlaidItemLoginRequired, "ITEM_LOGIN_REQUIRED"); err != nil {
		t.Fatal(err)
	}
	it, _ = s.GetPlaidItem(ctx, uid, itemID)
	if it.SyncCursor != "cursor-1" || it.Status != PlaidItemLoginRequired || it.LastError != "ITEM_LOGIN_REQUIRED" {
		t.Errorf("after updates: %+v", it)
	}
	if it.LastSyncedAt == nil {
		t.Error("cursor update should stamp last_synced_at")
	}

	// Worker list honors status + cutoff.
	if items, _ := s.ListActivePlaidItems(ctx, time.Now().Add(time.Hour)); len(items) != 0 {
		t.Errorf("login_required item in active list: %+v", items)
	}
	_ = s.SetPlaidItemStatus(ctx, itemID, PlaidItemActive, "")
	if items, _ := s.ListActivePlaidItems(ctx, time.Now().Add(time.Hour)); len(items) != 1 {
		t.Error("active item missing from worker list")
	}
	if items, _ := s.ListActivePlaidItems(ctx, time.Now().Add(-time.Hour)); len(items) != 0 {
		t.Error("recently synced item should be excluded by cutoff")
	}

	// Unlink then delete.
	if err := s.UnlinkAccountFromPlaid(ctx, uid, acctID); err != nil {
		t.Fatal(err)
	}
	a, _ = s.GetAccount(ctx, uid, acctID)
	if a.PlaidLinked() || a.PlaidSyncFrom != nil {
		t.Errorf("account still linked after unlink: %+v", a)
	}
	if err := s.DeletePlaidItem(ctx, uid, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPlaidItem(ctx, uid, itemID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("item survives delete: err = %v", err)
	}
}

// TestWipeUserDataWithPlaidItem guards the delete order: accounts reference
// plaid_items, so the wipe must remove accounts before items.
func TestWipeUserDataWithPlaidItem(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	itemID, err := s.CreatePlaidItem(ctx, PlaidItem{
		UserID: uid, PlaidItemID: "item-wipe", AccessTokenEncrypted: []byte{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	acctID, err := s.CreateAccount(ctx, uid, Account{Name: "Linked", Type: TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkAccountToPlaid(ctx, uid, acctID, itemID, "pa-1", time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := s.WipeUserData(ctx, uid); err != nil {
		t.Fatalf("WipeUserData with linked plaid item: %v", err)
	}
	if _, err := s.GetPlaidItem(ctx, uid, itemID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("plaid item survives wipe: err = %v", err)
	}

	// DeleteUser path too.
	itemID2, _ := s.CreatePlaidItem(ctx, PlaidItem{UserID: uid, PlaidItemID: "item-del", AccessTokenEncrypted: []byte{1}})
	acctID2, _ := s.CreateAccount(ctx, uid, Account{Name: "Linked2", Type: TypeChecking})
	if err := s.LinkAccountToPlaid(ctx, uid, acctID2, itemID2, "pa-2", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, uid); err != nil {
		t.Fatalf("DeleteUser with linked plaid item: %v", err)
	}
}
