package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/auth"
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
// lock, migrates, truncates, and re-seeds the global Income group/category.
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

func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	st := store.New(openTestDB(t))
	svc := auth.NewService(st, mail.NewConsole(), "http://localhost:8080", auth.Config{})
	return New(st, svc, false, 3600)
}

func postForm(t *testing.T, r http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	return w
}

func TestSignupThenLoginBlockedUntilVerified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)
	r := gin.New()
	r.POST("/signup", h.Signup)
	r.POST("/login", h.Login)

	form := url.Values{"email": {"x@example.com"}, "password": {"password1"}}

	w := postForm(t, r, "/signup", form)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Check your email") {
		t.Fatalf("signup: %d %s", w.Code, w.Body.String())
	}

	// Login before verification must be rejected.
	w = postForm(t, r, "/login", form)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("login should be blocked until verified: %d", w.Code)
	}
}

func TestSignupRejectsShortPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)
	r := gin.New()
	r.POST("/signup", h.Signup)

	w := postForm(t, r, "/signup", url.Values{"email": {"y@example.com"}, "password": {"short"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short password: got %d, want 400", w.Code)
	}
}

func TestChangePassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandlers(t)

	ctx := context.Background()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := h.store.CreateUser(ctx, "acc@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uid); c.Set("userEmail", "acc@example.com") })
	r.POST("/account/password", h.ChangePassword)

	// Wrong current password is rejected.
	w := postForm(t, r, "/account/password", url.Values{"current": {"wrong"}, "next": {"newpassword"}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong current: got %d, want 400", w.Code)
	}

	// Correct current password succeeds and actually changes the hash.
	w = postForm(t, r, "/account/password", url.Values{"current": {"password1"}, "next": {"newpassword"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Password changed") {
		t.Fatalf("change password: %d %s", w.Code, w.Body.String())
	}
	u, _ := h.store.GetUserByID(ctx, uid)
	if ok, _ := auth.VerifyPassword(u.PasswordHash, "newpassword"); !ok {
		t.Fatal("password hash was not updated to the new password")
	}
}
