package janitor_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/janitor"
	"github.com/sbengtson/budget/internal/core/store"
)

// testDBLockKey MUST match the other packages' advisory-lock key so all
// DB-backed tests serialize on the shared budget_test database.
const testDBLockKey = 918273645

var testTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "auth_lockouts", "auth_challenges", "recovery_codes", "user_totp", "webauthn_credentials", "oauth_identities", "sessions", "users",
}

func testDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
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

func TestSweepRemovesExpiredAndKeepsLive(t *testing.T) {
	conn := openTestDB(t)
	s := store.New(conn)
	ctx := context.Background()

	uid, err := s.CreateUser(ctx, "janitor@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, uid, "live-session", time.Now().Add(time.Hour), store.SessionInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, uid, "dead-session", time.Now().Add(-time.Hour), store.SessionInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateVerificationToken(ctx, uid, "live-token", "email_verify", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateVerificationToken(ctx, uid, "dead-token", "email_verify", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := janitor.New(s).Sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if _, err := s.GetSessionByTokenHash(ctx, "live-session"); err != nil {
		t.Fatal("an unexpired session must survive the sweep")
	}
	if _, err := s.GetSessionByTokenHash(ctx, "dead-session"); err == nil {
		t.Fatal("an expired session should have been pruned")
	}

	var tokens int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM verification_tokens`).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 1 {
		t.Fatalf("verification_tokens = %d, want 1 (only the live one)", tokens)
	}
}

// Pruning must never release a lock that is still in force — that would turn
// housekeeping into a way to shorten an attacker's wait.
func TestSweepKeepsLiveLockouts(t *testing.T) {
	conn := openTestDB(t)
	s := store.New(conn)
	ctx := context.Background()

	if _, err := s.RecordLoginFailure(ctx, store.ScopePasswordLogin, "held@example.com", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.LockSubject(ctx, store.ScopePasswordLogin, "held@example.com", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := janitor.New(s).Sweep(ctx); err != nil {
		t.Fatal(err)
	}

	l, err := s.GetLockout(ctx, store.ScopePasswordLogin, "held@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !l.Locked(time.Now()) {
		t.Fatal("a live lock must survive the sweep")
	}
}

func TestSweepIsIdempotent(t *testing.T) {
	conn := openTestDB(t)
	s := store.New(conn)
	ctx := context.Background()
	j := janitor.New(s)

	for i := range 3 {
		if err := j.Sweep(ctx); err != nil {
			t.Fatalf("sweep %d: %v", i+1, err)
		}
	}
}
