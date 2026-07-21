package auth_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/mail"
	"github.com/sbengtson/budget/internal/core/store"
)

type capMailer struct{ last mail.Message }

func (c *capMailer) Send(_ context.Context, m mail.Message) error { c.last = m; return nil }

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
// lock, migrates, truncates, and re-seeds the global Income group/category
// (NULL user_id, as migration 00005 leaves it) so the first registered user can
// claim it via ClaimOrphanData.
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

func newService(t *testing.T) (*auth.Service, *capMailer, *store.Store) {
	t.Helper()
	st := store.New(openTestDB(t))
	m := &capMailer{}
	svc := auth.NewService(st, m, "http://localhost:8080", auth.Config{})
	return svc, m, st
}

func TestRegisterVerifyLogin(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()

	if err := svc.Register(ctx, "A@Example.com ", "pw-secret"); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Login before verification is rejected.
	if _, err := svc.Login(ctx, "a@example.com", "pw-secret", "ua", "ip"); err != auth.ErrEmailNotVerified {
		t.Fatalf("want ErrEmailNotVerified, got %v", err)
	}
	// Extract token from the verify email link.
	token := linkToken(t, m.last.Text, "token=")
	if err := svc.VerifyEmail(ctx, token); err != nil {
		t.Fatalf("verify: %v", err)
	}
	tok, err := svc.Login(ctx, "a@example.com", "pw-secret", "ua", "ip")
	if err != nil || tok == "" {
		t.Fatalf("login after verify: tok=%q err=%v", tok, err)
	}
}

func TestSecondUserGetsOwnIncomeCategory(t *testing.T) {
	svc, _, st := newService(t)
	ctx := context.Background()

	// First user (owner) claims the migration-seeded global Income category.
	if err := svc.Register(ctx, "owner@example.com", "password1"); err != nil {
		t.Fatal(err)
	}
	// Second user must get their OWN Income category, not the owner's.
	if err := svc.Register(ctx, "second@example.com", "password2"); err != nil {
		t.Fatal(err)
	}

	countIncome := func(email string) int {
		u, err := st.GetUserByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		cats, err := st.ListCategories(ctx, u.ID, false)
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

	if got := countIncome("second@example.com"); got != 1 {
		t.Fatalf("second user income categories = %d, want 1", got)
	}
	if got := countIncome("owner@example.com"); got != 1 {
		t.Fatalf("owner income categories = %d, want 1 (no duplicate from seeding)", got)
	}
}

func TestDuplicateEmail(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	_ = svc.Register(ctx, "dup@example.com", "pw")
	if err := svc.Register(ctx, "dup@example.com", "pw"); err != auth.ErrEmailTaken {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
}

func TestForgotReset(t *testing.T) {
	svc, m, _ := newService(t)
	ctx := context.Background()
	_ = svc.Register(ctx, "r@example.com", "old-pw")
	_ = svc.VerifyEmail(ctx, linkToken(t, m.last.Text, "token="))

	if err := svc.RequestPasswordReset(ctx, "r@example.com"); err != nil {
		t.Fatal(err)
	}
	resetTok := linkToken(t, m.last.Text, "token=")
	if err := svc.ResetPassword(ctx, resetTok, "new-pw"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := svc.Login(ctx, "r@example.com", "new-pw", "ua", "ip"); err != nil {
		t.Fatalf("login with new pw: %v", err)
	}
	if _, err := svc.Login(ctx, "r@example.com", "old-pw", "ua", "ip"); err != auth.ErrInvalidCredentials {
		t.Fatalf("old pw should fail: %v", err)
	}
}

// linkToken pulls the value after marker (e.g. "token=") from an email body.
func linkToken(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("marker %q not in body: %q", marker, body)
	}
	rest := body[i+len(marker):]
	if j := strings.IndexAny(rest, "\n \t\"<"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}
