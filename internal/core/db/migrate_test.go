package db

import (
	"database/sql"
	"os"
	"strings"
	"testing"
)

func migrateTestDSN() string {
	if u := os.Getenv("BUDGET_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/budget_test?sslmode=disable"
}

var migrateTestTables = []string{
	"transactions", "budgets", "categories", "category_groups",
	"incomes", "accounts", "verification_tokens", "sessions", "users",
}

// migrateTestLockKey MUST match the store/web packages' advisory-lock key so
// every DB-backed test across the parallel package binaries serializes on the
// shared budget_test database.
const migrateTestLockKey = 918273645

// openMigrateTestDB opens the shared Postgres test DB under a global advisory
// lock (so its destructive up/down migrations don't race the store/web package
// tests that share the same database when `go test ./...` runs them in
// parallel), applies migrations, and truncates every table for a clean slate.
func openMigrateTestDB(t *testing.T) (*sql.DB, Dialect) {
	t.Helper()

	lockConn, _, err := Open(migrateTestDSN(), false)
	if err != nil {
		t.Fatalf("open lock conn: %v", err)
	}
	lockConn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = lockConn.Close() })
	if _, err := lockConn.Exec("SELECT pg_advisory_lock($1)", migrateTestLockKey); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}

	conn, dialect, err := Open(migrateTestDSN(), false)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := MigrateUp(conn, dialect); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if _, err := conn.Exec("TRUNCATE TABLE " + strings.Join(migrateTestTables, ", ") + " RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return conn, dialect
}

// tableExists reports whether a table of the given name exists in the current schema.
func tableExists(t *testing.T, conn *sql.DB, tbl string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = current_schema() AND table_name = $1`, tbl).Scan(&n); err != nil {
		t.Fatalf("information_schema.tables(%s): %v", tbl, err)
	}
	return n > 0
}

// hasUserIDColumn reports whether the given table carries a user_id column.
func hasUserIDColumn(t *testing.T, conn *sql.DB, tbl string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'user_id'`, tbl).Scan(&n); err != nil {
		t.Fatalf("information_schema.columns(%s): %v", tbl, err)
	}
	return n == 1
}

var scopedTables = []string{"accounts", "category_groups", "categories", "transactions", "budgets", "incomes"}

// TestMigrateAuthSchema runs all migrations on the test DB and asserts the 00007
// auth objects exist and the six data tables carry a user_id column.
func TestMigrateAuthSchema(t *testing.T) {
	conn, _ := openMigrateTestDB(t)

	for _, tbl := range []string{"users", "sessions", "verification_tokens"} {
		if !tableExists(t, conn, tbl) {
			t.Errorf("expected table %q to exist", tbl)
		}
	}

	for _, tbl := range scopedTables {
		if !hasUserIDColumn(t, conn, tbl) {
			t.Errorf("table %q missing user_id column", tbl)
		}
	}
}

// TestMigrateAuthRoundTrip seeds data, rolls 00007 back to 00006, verifies the
// reverse migration preserved the seeded rows and removed the auth schema, then
// re-applies 00007 and verifies the auth schema is back.
func TestMigrateAuthRoundTrip(t *testing.T) {
	conn, dialect := openMigrateTestDB(t)

	// Seed one row into a table that gains user_id (accounts) and another (incomes).
	if _, err := conn.Exec(
		`INSERT INTO accounts (name, type, starting_balance_cents) VALUES ('Checking','checking',500)`,
	); err != nil {
		t.Fatalf("seed accounts: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO incomes (month, name, amount_cents) VALUES ('2026-07','Salary',100000)`,
	); err != nil {
		t.Fatalf("seed incomes: %v", err)
	}

	// 00008 (user name), 00009 (user avatar), 00010 (email change), 00011
	// (add-ons), 00012 (subscriptions), 00013 (billing exempt), 00014 (admin),
	// 00015 (user locale), 00016 (onboarded) and 00017 (estimates) sit on top of
	// the auth migration; peel them off first (17 -> ... -> 7) so 00007 is the
	// current head for the roundtrip below.
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00017: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00016: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00015: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00014: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00013: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00012: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00011: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00010: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00009: %v", err)
	}
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down 00008: %v", err)
	}

	// Roll back 00007 -> version 6.
	if err := MigrateDown(conn, dialect); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if v, err := MigrateVersion(conn, dialect); err != nil || v != 6 {
		t.Fatalf("expected version 6 after down, got %d (err %v)", v, err)
	}

	// Seeded data must survive the reverse migration.
	var accName, incName string
	if err := conn.QueryRow(`SELECT name FROM accounts WHERE name='Checking'`).Scan(&accName); err != nil {
		t.Errorf("seeded accounts row did not survive down migration: %v", err)
	}
	if err := conn.QueryRow(`SELECT name FROM incomes WHERE name='Salary'`).Scan(&incName); err != nil {
		t.Errorf("seeded incomes row did not survive down migration: %v", err)
	}

	// Auth tables must be gone.
	for _, tbl := range []string{"users", "sessions", "verification_tokens"} {
		if tableExists(t, conn, tbl) {
			t.Errorf("expected table %q to be dropped after down", tbl)
		}
	}
	// user_id columns must be removed from every scoped table.
	for _, tbl := range scopedTables {
		if hasUserIDColumn(t, conn, tbl) {
			t.Errorf("table %q still has user_id column after down", tbl)
		}
	}

	// Re-apply 00007 -> version 7.
	if err := MigrateUpByOne(conn, dialect); err != nil {
		t.Fatalf("migrate up-by-one: %v", err)
	}
	if v, err := MigrateVersion(conn, dialect); err != nil || v != 7 {
		t.Fatalf("expected version 7 after up, got %d (err %v)", v, err)
	}
	if !tableExists(t, conn, "users") {
		t.Errorf("users table not restored after re-applying 00007")
	}
	if !hasUserIDColumn(t, conn, "accounts") {
		t.Errorf("accounts.user_id not restored after re-applying 00007")
	}
}

// TestMigrateAuthCompositeUnique proves the accounts uniqueness changed from a
// global name-unique to a composite (user_id, name): the same name under two
// different users is allowed, while a duplicate (user_id, name) is rejected.
func TestMigrateAuthCompositeUnique(t *testing.T) {
	conn, _ := openMigrateTestDB(t)

	for _, email := range []string{"a@example.com", "b@example.com"} {
		if _, err := conn.Exec(
			`INSERT INTO users (email, password_hash) VALUES ($1, 'x')`, email,
		); err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
	}

	// Same name under different users: allowed (global name-unique is gone).
	if _, err := conn.Exec(
		`INSERT INTO accounts (name, type, user_id) VALUES ('Shared','checking',1)`,
	); err != nil {
		t.Fatalf("insert account (user 1): %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO accounts (name, type, user_id) VALUES ('Shared','checking',2)`,
	); err != nil {
		t.Errorf("same name under a different user should be allowed, got: %v", err)
	}

	// Same (user_id, name): rejected (composite uniqueness enforced).
	_, err := conn.Exec(
		`INSERT INTO accounts (name, type, user_id) VALUES ('Shared','checking',1)`,
	)
	if err == nil {
		t.Errorf("duplicate (user_id, name) should violate the composite unique constraint")
	} else if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Errorf("expected a uniqueness error, got: %v", err)
	}
}
