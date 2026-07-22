package apiv1

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/billing"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

// testDBLockKey MUST match the store/db/web packages' advisory-lock key so all
// DB-backed tests serialize on the shared budget_test database.
const testDBLockKey = 918273645

func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "sessions", "users",
}

// openTestDB opens the shared Postgres test database under a global advisory
// lock, migrates, and truncates. (No Income seed needed here — these tests don't
// touch budget data.)
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

// newTestAPI builds an API over the shared test DB with billing disabled (empty
// price), plus a mounted router matching production wiring (/api/v1 group).
func newTestAPI(t *testing.T) (*API, *store.Store, http.Handler) {
	t.Helper()
	a, st, _, r := newTestAPIWithBilling(t, billingDisabled)
	return a, st, r
}

type billingMode int

const (
	billingDisabled billingMode = iota // empty Stripe config → gate is a no-op
	billingEnabled                     // dummy (non-empty) config → gate is live, no Stripe calls
)

// newTestAPIWithBilling builds the API + mounted router over a fresh test DB,
// with billing either inert or "live". In the enabled mode the Stripe config is
// non-empty but bogus, which is fine because the access gate only reads the
// store — it never calls Stripe. The *sql.DB is returned so a test can set
// DB-only state (e.g. billing_exempt) that has no store setter.
func newTestAPIWithBilling(t *testing.T, mode billingMode) (*API, *store.Store, *sql.DB, http.Handler) {
	t.Helper()
	conn := openTestDB(t)
	st := store.New(conn)
	svc := auth.NewService(st, mail.NewConsole(), "http://localhost:8080", auth.Config{})

	secret, price := "", ""
	if mode == billingEnabled {
		secret, price = "sk_test_dummy", "price_dummy"
	}
	bill := billing.NewService(st, secret, price, "http://localhost:8080", "")
	a := New(st, svc, bill)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	a.Register(r.Group("/api/v1"))
	return a, st, conn, r
}

// makeVerifiedUser creates a user with a verified email and returns its id.
func makeVerifiedUser(t *testing.T, st *store.Store, email, password string) int64 {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := st.CreateUser(ctx, email, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	return uid
}

// doJSON issues a request with an optional bearer token and JSON body, decoding
// the response into out (when non-nil). Returns the recorder for status checks.
func doJSON(t *testing.T, r http.Handler, method, path, token, body string, out any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if out != nil && w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s: %v (body=%s)", method, path, err, w.Body.String())
		}
	}
	return w
}

func TestHealth(t *testing.T) {
	_, _, r := newTestAPI(t)
	var resp struct {
		Status string `json:"status"`
	}
	w := doJSON(t, r, http.MethodGet, "/api/v1/health", "", "", &resp)
	if w.Code != http.StatusOK || resp.Status != "ok" {
		t.Fatalf("health: %d %s", w.Code, w.Body.String())
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	_, st, r := newTestAPI(t)
	makeVerifiedUser(t, st, "user@example.com", "supersecret")

	var resp errorBody
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"user@example.com","password":"wrong"}`, &resp)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if resp.Error.Code != "invalid_credentials" {
		t.Errorf("code = %q, want invalid_credentials", resp.Error.Code)
	}
}

func TestLoginUnverifiedEmail(t *testing.T) {
	_, st, r := newTestAPI(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("supersecret")
	if _, err := st.CreateUser(ctx, "unverified@example.com", hash); err != nil {
		t.Fatal(err)
	}

	var resp errorBody
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"unverified@example.com","password":"supersecret"}`, &resp)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if resp.Error.Code != "email_not_verified" {
		t.Errorf("code = %q, want email_not_verified", resp.Error.Code)
	}
}

func TestLoginMalformedBody(t *testing.T) {
	_, _, r := newTestAPI(t)
	var resp errorBody
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "", `not json`, &resp)
	if w.Code != http.StatusBadRequest || resp.Error.Code != "invalid_request" {
		t.Fatalf("malformed body: %d %q", w.Code, resp.Error.Code)
	}
}

func TestLoginSuccessThenMe(t *testing.T) {
	_, st, r := newTestAPI(t)
	uid := makeVerifiedUser(t, st, "user@example.com", "supersecret")

	var login authResponse
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"user@example.com","password":"supersecret"}`, &login)
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if login.Token == "" {
		t.Fatal("login returned an empty token")
	}
	if login.User.ID != uid || login.User.Email != "user@example.com" {
		t.Errorf("login user = %+v, want id=%d email=user@example.com", login.User, uid)
	}

	// The returned token authenticates /me.
	var me meResponse
	w = doJSON(t, r, http.MethodGet, "/api/v1/me", login.Token, "", &me)
	if w.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if me.User.ID != uid {
		t.Errorf("me user id = %d, want %d", me.User.ID, uid)
	}
}

func TestMeRejectsMissingAndBadToken(t *testing.T) {
	_, _, r := newTestAPI(t)

	// No Authorization header.
	var resp errorBody
	w := doJSON(t, r, http.MethodGet, "/api/v1/me", "", "", &resp)
	if w.Code != http.StatusUnauthorized || resp.Error.Code != "unauthorized" {
		t.Errorf("missing token: %d %q", w.Code, resp.Error.Code)
	}

	// Garbage token.
	resp = errorBody{}
	w = doJSON(t, r, http.MethodGet, "/api/v1/me", "garbage-token", "", &resp)
	if w.Code != http.StatusUnauthorized || resp.Error.Code != "unauthorized" {
		t.Errorf("bad token: %d %q", w.Code, resp.Error.Code)
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	_, st, r := newTestAPI(t)
	makeVerifiedUser(t, st, "user@example.com", "supersecret")

	var login authResponse
	doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"user@example.com","password":"supersecret"}`, &login)
	if login.Token == "" {
		t.Fatal("no token from login")
	}

	// Token works before logout.
	if w := doJSON(t, r, http.MethodGet, "/api/v1/me", login.Token, "", nil); w.Code != http.StatusOK {
		t.Fatalf("me before logout = %d, want 200", w.Code)
	}

	// Logout, then the same token is rejected.
	if w := doJSON(t, r, http.MethodPost, "/api/v1/logout", login.Token, "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", w.Code)
	}
	if w := doJSON(t, r, http.MethodGet, "/api/v1/me", login.Token, "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", w.Code)
	}
}

// login is a small helper: authenticate and return the bearer token.
func login(t *testing.T, r http.Handler, email, password string) string {
	t.Helper()
	var resp authResponse
	w := doJSON(t, r, http.MethodPost, "/api/v1/login", "",
		`{"email":"`+email+`","password":"`+password+`"}`, &resp)
	if w.Code != http.StatusOK || resp.Token == "" {
		t.Fatalf("login %s: %d %s", email, w.Code, w.Body.String())
	}
	return resp.Token
}

func TestSubscribedGateDeniesWhenBillingLiveAndNoSubscription(t *testing.T) {
	_, st, _, r := newTestAPIWithBilling(t, billingEnabled)
	makeVerifiedUser(t, st, "broke@example.com", "supersecret")

	// Billing is live and this user has no subscription and isn't exempt.
	var me meResponse
	token := login(t, r, "broke@example.com", "supersecret")
	if w := doJSON(t, r, http.MethodGet, "/api/v1/me", token, "", &me); w.Code != http.StatusOK {
		t.Fatalf("me: %d", w.Code)
	}
	if me.Subscribed {
		t.Error("subscribed = true for a user with no subscription under live billing")
	}
}

func TestBudget(t *testing.T) {
	_, st, r := newTestAPI(t)
	uid := makeVerifiedUser(t, st, "user@example.com", "supersecret")
	token := login(t, r, "user@example.com", "supersecret")
	ctx := context.Background()

	gid, err := st.CreateGroup(ctx, uid, "Bills", 0)
	if err != nil {
		t.Fatal(err)
	}
	catID, err := st.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Rent", SortOrder: 0})
	if err != nil {
		t.Fatal(err)
	}
	const month = "2026-07"
	if err := st.SetAssigned(ctx, uid, month, catID, 150000); err != nil {
		t.Fatal(err)
	}

	var resp budgetResponse
	w := doJSON(t, r, http.MethodGet, "/api/v1/budget?month="+month, token, "", &resp)
	if w.Code != http.StatusOK {
		t.Fatalf("budget: %d %s", w.Code, w.Body.String())
	}
	if resp.Month != month || resp.PrevMonth != "2026-06" || resp.NextMonth != "2026-08" {
		t.Errorf("months = prev=%s cur=%s next=%s", resp.PrevMonth, resp.Month, resp.NextMonth)
	}
	if resp.Summary.BudgetedCents != 150000 || resp.Summary.RemainingCents != -150000 {
		t.Errorf("summary = %+v (income 0, budgeted 150000, remaining -150000)", resp.Summary)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].Name != "Bills" {
		t.Fatalf("groups = %+v, want one 'Bills'", resp.Groups)
	}
	cats := resp.Groups[0].Categories
	if len(cats) != 1 || cats[0].Name != "Rent" || cats[0].AssignedCents != 150000 {
		t.Errorf("categories = %+v, want one Rent assigned 150000", cats)
	}
}

func TestBudgetRejectsBadMonth(t *testing.T) {
	_, st, r := newTestAPI(t)
	makeVerifiedUser(t, st, "user@example.com", "supersecret")
	token := login(t, r, "user@example.com", "supersecret")

	var resp errorBody
	w := doJSON(t, r, http.MethodGet, "/api/v1/budget?month=July", token, "", &resp)
	if w.Code != http.StatusBadRequest || resp.Error.Code != "invalid_request" {
		t.Fatalf("bad month: %d %q", w.Code, resp.Error.Code)
	}
}

func TestMeIncludesEnabledAddOns(t *testing.T) {
	_, st, r := newTestAPI(t)
	uid := makeVerifiedUser(t, st, "user@example.com", "supersecret")

	// A fresh user has no add-ons: the field is present and empty (not null).
	var me meResponse
	token := login(t, r, "user@example.com", "supersecret")
	doJSON(t, r, http.MethodGet, "/api/v1/me", token, "", &me)
	if me.AddOns == nil || len(me.AddOns) != 0 {
		t.Fatalf("fresh user addOns = %v, want []", me.AddOns)
	}

	// Enabling paydown surfaces it in /me.
	if err := st.SetAddOnEnabled(context.Background(), uid, "paydown", true); err != nil {
		t.Fatalf("enable paydown: %v", err)
	}
	me = meResponse{}
	doJSON(t, r, http.MethodGet, "/api/v1/me", token, "", &me)
	if len(me.AddOns) != 1 || me.AddOns[0] != "paydown" {
		t.Errorf("addOns after enabling = %v, want [paydown]", me.AddOns)
	}
}

func TestSubscribedGateAllowsBillingExemptUser(t *testing.T) {
	_, st, conn, r := newTestAPIWithBilling(t, billingEnabled)
	uid := makeVerifiedUser(t, st, "comp@example.com", "supersecret")

	// Flag the account complimentary (DB-only, no store setter by design).
	if _, err := conn.Exec("UPDATE users SET billing_exempt = TRUE WHERE id = $1", uid); err != nil {
		t.Fatalf("set exempt: %v", err)
	}

	var me meResponse
	token := login(t, r, "comp@example.com", "supersecret")
	if w := doJSON(t, r, http.MethodGet, "/api/v1/me", token, "", &me); w.Code != http.StatusOK {
		t.Fatalf("me: %d", w.Code)
	}
	if !me.Subscribed {
		t.Error("subscribed = false for a billing_exempt account")
	}
}
