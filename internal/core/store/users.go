package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
)

type User struct {
	ID              int64
	Email           string
	Name            string
	PasswordHash    string
	EmailVerifiedAt *time.Time
	AvatarUpdatedAt *time.Time // nil when the user has no custom avatar
	IsAdmin         bool
	DisabledAt      *time.Time // non-nil when the account is suspended
	Locale          i18n.Locale
	OnboardedAt     *time.Time // nil until the first-run wizard is finished
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Disabled reports whether the account is suspended. A disabled user cannot log
// in and their existing sessions are rejected (see auth.AuthenticateSession).
func (u User) Disabled() bool { return u.DisabledAt != nil }

// AvatarVersion returns a cache-busting version for the user's avatar URL — the
// avatar's last-updated unix time, or 0 when there's no custom avatar.
func (u User) AvatarVersion() int64 {
	if u.AvatarUpdatedAt == nil {
		return 0
	}
	return u.AvatarUpdatedAt.Unix()
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

// SetPendingEmail records a not-yet-verified email change for the user. The
// change only takes effect once the new address is confirmed (ApplyEmailChange).
func (s *Store) SetPendingEmail(ctx context.Context, userID int64, email string) error {
	_, err := s.run(ctx,
		`UPDATE users SET pending_email = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, email, userID)
	return err
}

// GetPendingEmail returns the user's pending (unconfirmed) email change, or ""
// when none is outstanding.
func (s *Store) GetPendingEmail(ctx context.Context, userID int64) (string, error) {
	var pending sql.NullString
	if err := s.queryOne(ctx, `SELECT pending_email FROM users WHERE id = $1`, userID).Scan(&pending); err != nil {
		return "", err
	}
	return pending.String, nil
}

// ApplyEmailChange promotes a confirmed pending email to the user's login email,
// clears the pending value, and marks the new address verified.
func (s *Store) ApplyEmailChange(ctx context.Context, userID int64, newEmail string) error {
	_, err := s.run(ctx,
		`UPDATE users
		 SET email = $1, pending_email = NULL, email_verified_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $2`, newEmail, userID)
	return err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.queryOne(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// userColumns is the SELECT list every user lookup shares, kept in one place so
// it cannot drift out of step with scanUser's Scan order.
const userColumns = `id, email, name, password_hash, email_verified_at, avatar_updated_at,
	is_admin, disabled_at, locale, onboarded_at, created_at, updated_at`

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return s.scanUser(s.queryOne(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1`, email))
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.queryOne(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	var verified, avatarUpdated, disabled, onboarded nullTime
	var locale string
	// password_hash is nullable since 00021: passkey-only and OAuth-only users
	// have no password at all. An absent hash reads as "", which VerifyPassword
	// rejects cleanly rather than erroring.
	var passwordHash sql.NullString
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &passwordHash, &verified, &avatarUpdated,
		&u.IsAdmin, &disabled, &locale, &onboarded, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return User{}, err
	}
	u.PasswordHash = passwordHash.String
	u.EmailVerifiedAt = verified.Ptr()
	u.AvatarUpdatedAt = avatarUpdated.Ptr()
	u.DisabledAt = disabled.Ptr()
	u.OnboardedAt = onboarded.Ptr()
	// A locale we no longer ship (a dropped language, a hand-edited row) falls
	// back to the default rather than propagating an unsupported tag into the
	// renderer, where every lookup would miss.
	u.Locale, _ = i18n.Parse(locale)
	return u, nil
}

// Onboarded reports whether the user has finished the first-run wizard.
func (u User) Onboarded() bool { return u.OnboardedAt != nil }

// MarkOnboarded records that the user finished the first-run wizard, so they
// are never sent through it again. Idempotent — re-finishing does not move the
// timestamp, which keeps it meaningful as "when they first got set up".
func (s *Store) MarkOnboarded(ctx context.Context, userID int64) error {
	_, err := s.run(ctx,
		`UPDATE users SET onboarded_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND onboarded_at IS NULL`, userID)
	return err
}

// UpdateUserLocale sets the user's interface language. The caller is expected to
// have validated the tag; an unsupported one is rejected here rather than
// stored, so a bad request cannot leave an account stuck rendering message IDs.
func (s *Store) UpdateUserLocale(ctx context.Context, userID int64, l i18n.Locale) error {
	if !l.Valid() {
		return fmt.Errorf("unsupported locale %q", l)
	}
	_, err := s.run(ctx,
		`UPDATE users SET locale = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, string(l), userID)
	return err
}

// SetUserAvatar stores the (already-processed) avatar PNG for the user and bumps
// its timestamp so the served URL's cache-busting version changes.
func (s *Store) SetUserAvatar(ctx context.Context, userID int64, png []byte) error {
	_, err := s.run(ctx,
		`UPDATE users SET avatar = $1, avatar_updated_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $2`, png, userID)
	return err
}

// RemoveUserAvatar clears the user's custom avatar (both the blob and its
// timestamp, so AvatarVersion drops to 0 and the monogram is shown again).
func (s *Store) RemoveUserAvatar(ctx context.Context, userID int64) error {
	_, err := s.run(ctx,
		`UPDATE users SET avatar = NULL, avatar_updated_at = NULL, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1`, userID)
	return err
}

// GetUserAvatar returns the stored avatar PNG bytes, or (nil, nil) when the user
// has no custom avatar.
func (s *Store) GetUserAvatar(ctx context.Context, userID int64) ([]byte, error) {
	var data []byte
	if err := s.queryOne(ctx, `SELECT avatar FROM users WHERE id = $1`, userID).Scan(&data); err != nil {
		return nil, err
	}
	return data, nil
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

// StarterGroup is one group of the starter budget offered by the first-run
// wizard. Key and Categories hold catalog keys, not display names: the wizard
// seeds in whatever language the user just chose, so the words cannot be baked
// in here. Resolve them with StarterGroupName / StarterCategoryName.
type StarterGroup struct {
	Key        string
	Categories []string
}

// StarterGroups is the starter budget layout, in display order. The wizard
// shows these for review and seeds the ones the user keeps; the system Income
// category is always created and is not among them.
var StarterGroups = []StarterGroup{
	{"bills", []string{"rent", "electric", "water", "internet", "phone"}},
	{"everyday", []string{"groceries", "transportation", "dining_out", "entertainment", "personal"}},
	{"savings", []string{"emergency_fund", "vacation"}},
}

// StarterGroupName and StarterCategoryName resolve a starter-budget key to its
// display name in a locale. These names become the user's own editable rows the
// moment they are inserted, so this is a one-time translation at seeding, not a
// rendering-time lookup — renaming a category later must not be undone by a
// language switch.
func StarterGroupName(l i18n.Locale, key string) string {
	return i18n.TIn(l, "starter.group."+key)
}

func StarterCategoryName(l i18n.Locale, key string) string {
	return i18n.TIn(l, "starter.category."+key)
}

// SeedNewUser provisions a user's starter budget in the given locale: a
// system-managed Income category plus the expense groups named in groupKeys
// (nil means all of StarterGroups).
//
// It is a no-op for parts the user already has: the Income category is created
// only when missing, and the expense groups only when the user has no
// non-income categories yet. So the first (owner) user, who inherits a real
// pre-existing budget via ClaimOrphanData, is left untouched, while a user with
// only the claimed system Income still gets the expense categories.
//
// One consequence, on the owner path only: that claimed Income category was
// inserted in English by migration 00005, and it is NOT renamed to match the
// language chosen in the wizard. Renaming is deliberately not attempted — the
// same claim can carry a real pre-existing budget whose category names are the
// owner's own words, and silently rewriting those would be the worse failure.
// Every subsequent user gets their Income category in their chosen language.
func (s *Store) SeedNewUser(ctx context.Context, userID int64, loc i18n.Locale, groupKeys []string) error {
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

	groups := StarterGroups
	if groupKeys != nil {
		want := make(map[string]bool, len(groupKeys))
		for _, k := range groupKeys {
			want[k] = true
		}
		// Filter StarterGroups rather than ranging over groupKeys, so the seeded
		// order is always the canonical display order and an unknown key coming
		// off a form is ignored instead of inserting a group named after a
		// missing message.
		groups = nil
		for _, g := range StarterGroups {
			if want[g.Key] {
				groups = append(groups, g)
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if incomeCount == 0 {
		incomeName := i18n.TIn(loc, "starter.group.income")
		gid, err := s.txInsertReturningID(ctx, tx,
			`INSERT INTO category_groups(user_id, name, sort_order) VALUES ($1, $2, -100)`, userID, incomeName)
		if err != nil {
			return fmt.Errorf("seed income group: %w", err)
		}
		// is_income is BOOLEAN: bind a Go bool via placeholder, never a literal.
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO categories(user_id, group_id, name, is_income, sort_order) VALUES ($1, $2, $3, $4, 0)`,
			userID, gid, i18n.TIn(loc, "starter.category.income"), true); err != nil {
			return fmt.Errorf("seed income category: %w", err)
		}
	}

	if expenseCount == 0 {
		for gi, g := range groups {
			gid, err := s.txInsertReturningID(ctx, tx,
				`INSERT INTO category_groups(user_id, name, sort_order) VALUES ($1, $2, $3)`,
				userID, StarterGroupName(loc, g.Key), gi)
			if err != nil {
				return fmt.Errorf("seed group %q: %w", g.Key, err)
			}
			for ci, key := range g.Categories {
				if _, err := s.txExec(ctx, tx,
					`INSERT INTO categories(user_id, group_id, name, sort_order) VALUES ($1, $2, $3, $4)`,
					userID, gid, StarterCategoryName(loc, key), ci); err != nil {
					return fmt.Errorf("seed category %q: %w", key, err)
				}
			}
		}
	}

	return tx.Commit()
}

// budgetTablesDeleteOrder lists the user-owned budget tables in child→parent
// order. These tables reference users(id) WITHOUT ON DELETE CASCADE (and each
// other), so a user's rows must be deleted in this order for the foreign keys to
// hold. The tricky edges: transactions → accounts + categories; budgets →
// categories; accounts → categories (payment_category_id, from the paydown
// add-on) + plaid_items (bank_sync add-on); categories → category_groups. So
// accounts must go before categories (not after) and before plaid_items, and
// category_groups is last. Callers deleting plaid_items rows are responsible
// for revoking the access token at Plaid (/item/remove) first.
var budgetTablesDeleteOrder = []string{
	"transactions", "budgets", "incomes", "accounts", "plaid_items", "categories", "category_groups",
}

// wipeUserDataTx deletes all of the user's budget content within tx. Shared by
// WipeUserData (start fresh) and DeleteUser (account deletion).
func (s *Store) wipeUserDataTx(ctx context.Context, tx *sql.Tx, userID int64) error {
	for _, tbl := range budgetTablesDeleteOrder {
		if _, err := s.txExec(ctx, tx, `DELETE FROM `+tbl+` WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("wipe %s: %w", tbl, err)
		}
	}
	return nil
}

// WipeUserData permanently deletes all of the user's budget content — accounts,
// category groups, categories, transactions, incomes and budget assignments — in
// a single transaction, leaving the user's account, login and subscription
// intact. This backs the "start fresh" action; callers typically follow it with
// SeedNewUser to re-provision the starter budget.
func (s *Store) WipeUserData(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.wipeUserDataTx(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteUser permanently deletes the user and all of their data. It wipes budget
// content first (those tables have no ON DELETE CASCADE) then deletes the user
// row, which cascades sessions, verification tokens, add-on links and
// subscription rows. The whole thing runs in one transaction, so a failure
// leaves the account untouched.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.wipeUserDataTx(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := s.txExec(ctx, tx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
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
