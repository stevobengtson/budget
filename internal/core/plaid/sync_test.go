package plaid

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	plaidapi "github.com/plaid/plaid-go/v45/plaid"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/crypto"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
)

// testDBLockKey MUST match the other packages' advisory-lock key so all
// DB-backed tests serialize on the shared budget_test database.
const testDBLockKey = 918273645

const testKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "plaid_items", "verification_tokens", "auth_lockouts", "auth_challenges", "recovery_codes", "user_totp", "webauthn_credentials", "sessions", "users",
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
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
	if err := db.MigrateUp(conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := conn.Exec("TRUNCATE TABLE " + strings.Join(testTables, ", ") + " RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return conn
}

// fakeAPI scripts TransactionsSync responses page by page. A page with
// errCode set fails instead of returning data.
type fakePage struct {
	resp    plaidapi.TransactionsSyncResponse
	errCode string
}

type fakeAPI struct {
	pages []fakePage
	calls int
}

type fakePlaidErr struct{ code string }

func (e fakePlaidErr) Error() string          { return "plaid: " + e.code }
func (e fakePlaidErr) PlaidErrorCode() string { return e.code }

func (f *fakeAPI) TransactionsSync(_ context.Context, _ plaidapi.TransactionsSyncRequest) (plaidapi.TransactionsSyncResponse, error) {
	if f.calls >= len(f.pages) {
		return plaidapi.TransactionsSyncResponse{}, fakePlaidErr{code: "INTERNAL_SERVER_ERROR"}
	}
	p := f.pages[f.calls]
	f.calls++
	if p.errCode != "" {
		return plaidapi.TransactionsSyncResponse{}, fakePlaidErr{code: p.errCode}
	}
	return p.resp, nil
}

func (f *fakeAPI) LinkTokenCreate(context.Context, plaidapi.LinkTokenCreateRequest) (plaidapi.LinkTokenCreateResponse, error) {
	return plaidapi.LinkTokenCreateResponse{}, nil
}
func (f *fakeAPI) ItemPublicTokenExchange(context.Context, string) (plaidapi.ItemPublicTokenExchangeResponse, error) {
	return plaidapi.ItemPublicTokenExchangeResponse{}, nil
}
func (f *fakeAPI) AccountsGet(context.Context, string) (plaidapi.AccountsGetResponse, error) {
	return plaidapi.AccountsGetResponse{}, nil
}
func (f *fakeAPI) ItemRemove(context.Context, string) error { return nil }
func (f *fakeAPI) WebhookVerificationKeyGet(context.Context, string) (plaidapi.WebhookVerificationKeyGetResponse, error) {
	return plaidapi.WebhookVerificationKeyGetResponse{}, nil
}

// newTestService builds a Service around the fake API and a real store.
func newTestService(t *testing.T, api api) (*Service, *store.Store, int64) {
	t.Helper()
	st := store.New(openTestDB(t))
	sealer, err := crypto.NewSealer(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("password1")
	uid, err := st.CreateUser(context.Background(), "plaid@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{api: api, store: st, sealer: sealer, days: 90, configured: true}, st, uid
}

// linkTestAccount creates an item + linked account for uid.
func linkTestAccount(t *testing.T, s *Service, st *store.Store, uid int64, plaidAcctID string, syncFrom time.Time) (store.PlaidItem, int64) {
	t.Helper()
	ctx := context.Background()
	sealed, _ := s.sealer.Seal("access-token-test")
	itemID, err := st.CreatePlaidItem(ctx, store.PlaidItem{
		UserID: uid, PlaidItemID: "item-" + plaidAcctID, AccessTokenEncrypted: sealed,
	})
	if err != nil {
		t.Fatal(err)
	}
	acctID, err := st.CreateAccount(ctx, uid, store.Account{Name: "Linked " + plaidAcctID, Type: store.TypeChecking})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LinkAccountToPlaid(ctx, uid, acctID, itemID, plaidAcctID, syncFrom); err != nil {
		t.Fatal(err)
	}
	item, err := st.GetPlaidItem(ctx, uid, itemID)
	if err != nil {
		t.Fatal(err)
	}
	return item, acctID
}

func plaidTx(id, acct, date string, amount float64, pending bool, pendingID string) plaidapi.Transaction {
	tx := plaidapi.Transaction{}
	tx.SetTransactionId(id)
	tx.SetAccountId(acct)
	tx.SetDate(date)
	tx.SetAmount(amount)
	tx.SetPending(pending)
	tx.SetName("Raw Bank Name")
	tx.SetMerchantName("Coffee Shop")
	if pendingID != "" {
		tx.SetPendingTransactionId(pendingID)
	}
	return tx
}

func syncPage(added []plaidapi.Transaction, modified []plaidapi.Transaction, removed []string, next string, hasMore bool) fakePage {
	resp := plaidapi.TransactionsSyncResponse{Added: added, Modified: modified, NextCursor: next, HasMore: hasMore}
	for _, id := range removed {
		r := plaidapi.RemovedTransaction{}
		r.SetTransactionId(id)
		resp.Removed = append(resp.Removed, r)
	}
	return fakePage{resp: resp}
}

func listTx(t *testing.T, st *store.Store, uid int64, acctID int64) []store.Transaction {
	t.Helper()
	txs, err := st.ListTransactions(context.Background(), uid, store.TxFilter{AccountID: &acctID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return txs
}

func TestSyncItemInitialBackfill(t *testing.T) {
	ctx := context.Background()
	syncFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	api := &fakeAPI{}
	s, st, uid := newTestService(t, api)
	item, acctID := linkTestAccount(t, s, st, uid, "pa-1", syncFrom)

	api.pages = []fakePage{
		// Two pages; second carries a pending tx, an out-of-window tx and an
		// unmapped-account tx.
		syncPage([]plaidapi.Transaction{
			plaidTx("t1", "pa-1", "2026-08-01", 12.34, false, ""),    // outflow, posted
			plaidTx("t2", "pa-1", "2026-08-02", -1000.00, false, ""), // inflow (paycheque)
		}, nil, nil, "c1", true),
		syncPage([]plaidapi.Transaction{
			plaidTx("t3", "pa-1", "2026-08-03", 5.00, true, ""),      // pending
			plaidTx("t4", "pa-1", "2026-06-15", 9.99, false, ""),     // before syncFrom: skipped
			plaidTx("t5", "pa-other", "2026-08-03", 7.77, false, ""), // unmapped: skipped
		}, nil, nil, "c2", false),
	}

	if err := s.SyncItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	txs := listTx(t, st, uid, acctID)
	if len(txs) != 3 {
		t.Fatalf("tx count = %d, want 3 (skips applied): %+v", len(txs), txs)
	}
	byPayee := map[string]store.Transaction{}
	for _, tx := range txs {
		byPayee[*tx.PlaidTransactionID] = tx
	}
	t1, t2, t3 := byPayee["t1"], byPayee["t2"], byPayee["t3"]
	if t1.OutflowCents != 1234 || t1.InflowCents != 0 || !t1.Cleared {
		t.Errorf("t1 mapping wrong: %+v", t1)
	}
	if t2.InflowCents != 100000 || t2.OutflowCents != 0 {
		t.Errorf("t2 inflow mapping wrong: %+v", t2)
	}
	if t3.Cleared {
		t.Error("pending t3 should be uncleared")
	}
	for id, tx := range byPayee {
		if tx.Payee == nil || *tx.Payee != "Coffee Shop" {
			t.Errorf("%s payee = %v, want merchant name", id, tx.Payee)
		}
		if tx.NeedsReview {
			t.Errorf("%s flagged needs_review on initial backfill", id)
		}
		if tx.CategoryID != nil {
			t.Errorf("%s should be uncategorized", id)
		}
	}

	// Cursor persisted from the final page.
	it, _ := st.GetPlaidItem(ctx, uid, item.ID)
	if it.SyncCursor != "c2" || it.LastSyncedAt == nil {
		t.Errorf("cursor = %q (synced %v), want c2", it.SyncCursor, it.LastSyncedAt)
	}
}

func TestSyncItemIncrementalPendingPostedRemoved(t *testing.T) {
	ctx := context.Background()
	syncFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	api := &fakeAPI{}
	s, st, uid := newTestService(t, api)
	item, acctID := linkTestAccount(t, s, st, uid, "pa-1", syncFrom)

	// Initial: one pending, one posted-to-be-removed.
	api.pages = []fakePage{
		syncPage([]plaidapi.Transaction{
			plaidTx("pend-1", "pa-1", "2026-08-01", 20.00, true, ""),
			plaidTx("gone-1", "pa-1", "2026-08-02", 5.00, false, ""),
		}, nil, nil, "c1", false),
	}
	if err := s.SyncItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	// User categorizes the pending transaction meanwhile.
	txs := listTx(t, st, uid, acctID)
	var pendRow store.Transaction
	for _, tx := range txs {
		if *tx.PlaidTransactionID == "pend-1" {
			pendRow = tx
		}
	}
	catID := seedCategory(t, st, uid)
	pendRow.CategoryID = &catID
	if err := st.UpdateTransaction(ctx, uid, pendRow); err != nil {
		t.Fatal(err)
	}

	// Incremental: pending posts under a new id (same amount), plus a brand-new
	// added tx; the other one is removed.
	item, _ = st.GetPlaidItem(ctx, uid, item.ID)
	api.pages = []fakePage{
		syncPage(
			[]plaidapi.Transaction{
				plaidTx("post-1", "pa-1", "2026-08-01", 20.00, false, "pend-1"),
				plaidTx("new-1", "pa-1", "2026-08-05", 8.00, false, ""),
			},
			nil,
			[]string{"gone-1"},
			"c2", false),
	}
	api.calls = 0
	if err := s.SyncItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	txs = listTx(t, st, uid, acctID)
	if len(txs) != 2 {
		t.Fatalf("tx count after incremental = %d, want 2: %+v", len(txs), txs)
	}
	byID := map[string]store.Transaction{}
	for _, tx := range txs {
		byID[*tx.PlaidTransactionID] = tx
	}
	if _, ok := byID["gone-1"]; ok {
		t.Error("removed transaction survived")
	}
	posted, ok := byID["post-1"]
	if !ok {
		t.Fatal("pending row was not re-pointed to posted id")
	}
	if !posted.Cleared {
		t.Error("posted row should be cleared")
	}
	if posted.CategoryID == nil || *posted.CategoryID != catID {
		t.Error("pending→posted lost the user's category")
	}
	if posted.NeedsReview {
		t.Error("unchanged amount/date must not re-flag review")
	}
	if !byID["new-1"].NeedsReview {
		t.Error("incremental adds must be flagged needs_review")
	}
}

func TestSyncItemMutationRestart(t *testing.T) {
	ctx := context.Background()
	api := &fakeAPI{}
	s, st, uid := newTestService(t, api)
	item, acctID := linkTestAccount(t, s, st, uid, "pa-1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	api.pages = []fakePage{
		syncPage([]plaidapi.Transaction{plaidTx("a1", "pa-1", "2026-08-01", 1.00, false, "")}, nil, nil, "c1", true),
		{errCode: "TRANSACTIONS_SYNC_MUTATION_DURING_PAGINATION"},
		// Restarted pass succeeds in one page.
		syncPage([]plaidapi.Transaction{plaidTx("a1", "pa-1", "2026-08-01", 1.00, false, "")}, nil, nil, "c9", false),
	}
	if err := s.SyncItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if txs := listTx(t, st, uid, acctID); len(txs) != 1 {
		t.Fatalf("tx count = %d, want 1 (no duplicates from restart)", len(txs))
	}
	it, _ := st.GetPlaidItem(ctx, uid, item.ID)
	if it.SyncCursor != "c9" {
		t.Errorf("cursor = %q, want c9 from restarted pass", it.SyncCursor)
	}
}

func TestSyncItemLoginRequired(t *testing.T) {
	ctx := context.Background()
	api := &fakeAPI{}
	s, st, uid := newTestService(t, api)
	item, _ := linkTestAccount(t, s, st, uid, "pa-1", time.Now())

	api.pages = []fakePage{{errCode: "ITEM_LOGIN_REQUIRED"}}
	if err := s.SyncItem(ctx, item); err == nil {
		t.Fatal("expected error")
	}
	it, _ := st.GetPlaidItem(ctx, uid, item.ID)
	if it.Status != store.PlaidItemLoginRequired {
		t.Errorf("status = %q, want login_required", it.Status)
	}
	if it.SyncCursor != "" {
		t.Errorf("cursor advanced on failure: %q", it.SyncCursor)
	}
}

func seedCategory(t *testing.T, st *store.Store, uid int64) int64 {
	t.Helper()
	ctx := context.Background()
	gid, err := st.CreateGroup(ctx, uid, "Test Group", 0)
	if err != nil {
		t.Fatal(err)
	}
	cid, err := st.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Test Category"})
	if err != nil {
		t.Fatal(err)
	}
	return cid
}
