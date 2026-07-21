package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type User struct {
	ID              int64
	Email           string
	Name            string
	PasswordHash    string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (int64, error) {
	id, err := s.insertReturningID(ctx,
		`INSERT INTO users(email, password_hash) VALUES ($1, $2)`, email, passwordHash)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// UpdateUserName sets the user's display name.
func (s *Store) UpdateUserName(ctx context.Context, userID int64, name string) error {
	_, err := s.run(ctx,
		`UPDATE users SET name = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, name, userID)
	return err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.queryOne(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return s.scanUser(s.queryOne(ctx,
		`SELECT id, email, name, password_hash, email_verified_at, created_at, updated_at
		 FROM users WHERE email = $1`, email))
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.queryOne(ctx,
		`SELECT id, email, name, password_hash, email_verified_at, created_at, updated_at
		 FROM users WHERE id = $1`, id))
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	var verified nullTime
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &verified, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, err
	}
	u.EmailVerifiedAt = verified.Ptr()
	return u, nil
}

func (s *Store) SetEmailVerified(ctx context.Context, userID int64) error {
	_, err := s.run(ctx,
		`UPDATE users SET email_verified_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1`, userID)
	return err
}

func (s *Store) UpdatePasswordHash(ctx context.Context, userID int64, hash string) error {
	_, err := s.run(ctx,
		`UPDATE users SET password_hash = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		hash, userID)
	return err
}

// defaultGroups is the starter budget layout seeded for a new user (in addition
// to the system Income category). Order here is the display order.
var defaultGroups = []struct {
	Name       string
	Categories []string
}{
	{"Bills", []string{"Rent/Mortgage", "Electric", "Water", "Internet", "Phone"}},
	{"Everyday", []string{"Groceries", "Transportation", "Dining Out", "Entertainment", "Personal"}},
	{"Savings", []string{"Emergency Fund", "Vacation"}},
}

// SeedNewUser provisions a user's starter budget: a system-managed "Income"
// category plus a default set of expense groups/categories (see defaultGroups),
// so a new user has something to work with immediately.
//
// It is a no-op for parts the user already has: the Income category is created
// only when missing, and the default expense groups only when the user has no
// non-income categories yet. So the first (owner) user, who inherits a real
// pre-existing budget via ClaimOrphanData, is left untouched, while a user with
// only the claimed system Income still gets the default expense categories.
func (s *Store) SeedNewUser(ctx context.Context, userID int64) error {
	var incomeCount, expenseCount int
	if err := s.queryOne(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE is_income),
		   COUNT(*) FILTER (WHERE NOT is_income)
		 FROM categories WHERE user_id=$1`, userID).Scan(&incomeCount, &expenseCount); err != nil {
		return err
	}
	if incomeCount > 0 && expenseCount > 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if incomeCount == 0 {
		gid, err := s.txInsertReturningID(ctx, tx,
			`INSERT INTO category_groups(user_id, name, sort_order) VALUES ($1, 'Income', -100)`, userID)
		if err != nil {
			return fmt.Errorf("seed income group: %w", err)
		}
		// is_income is BOOLEAN: bind a Go bool via placeholder, never a literal.
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO categories(user_id, group_id, name, is_income, sort_order) VALUES ($1, $2, 'Income', $3, 0)`,
			userID, gid, true); err != nil {
			return fmt.Errorf("seed income category: %w", err)
		}
	}

	if expenseCount == 0 {
		for gi, g := range defaultGroups {
			gid, err := s.txInsertReturningID(ctx, tx,
				`INSERT INTO category_groups(user_id, name, sort_order) VALUES ($1, $2, $3)`, userID, g.Name, gi)
			if err != nil {
				return fmt.Errorf("seed group %q: %w", g.Name, err)
			}
			for ci, name := range g.Categories {
				if _, err := s.txExec(ctx, tx,
					`INSERT INTO categories(user_id, group_id, name, sort_order) VALUES ($1, $2, $3, $4)`,
					userID, gid, name, ci); err != nil {
					return fmt.Errorf("seed category %q: %w", name, err)
				}
			}
		}
	}

	return tx.Commit()
}

// ClaimOrphanData assigns every row with a NULL user_id (pre-auth data) to userID.
// Called once, for the first registered user (the owner). Idempotent: after the
// first claim there are no NULL rows left.
func (s *Store) ClaimOrphanData(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, tbl := range []string{"accounts", "category_groups", "categories", "transactions", "budgets", "incomes"} {
		if _, err := s.txExec(ctx, tx,
			`UPDATE `+tbl+` SET user_id = $1 WHERE user_id IS NULL`, userID); err != nil {
			return fmt.Errorf("claim %s: %w", tbl, err)
		}
	}
	return tx.Commit()
}
