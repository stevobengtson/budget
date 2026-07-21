// Package store is the persistence layer over the Postgres budget database.
// All SQL is written using native Postgres $1,$2,... placeholders.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) DB() *sql.DB { return s.db }

// ErrNotOwned is returned by a mutation when it references a row (account,
// category, or group) that does not belong to the acting user. Handlers should
// surface it as a client error (4xx), not a 500.
var ErrNotOwned = errors.New("referenced record does not belong to user")

// ownsAccount/ownsCategory/ownsGroup report whether the row id is owned by
// userID. Used to reject cross-tenant foreign-key references on writes.
func (s *Store) ownsAccount(ctx context.Context, userID, id int64) (bool, error) {
	return s.rowExists(ctx, "accounts", userID, id)
}

func (s *Store) ownsCategory(ctx context.Context, userID, id int64) (bool, error) {
	return s.rowExists(ctx, "categories", userID, id)
}

func (s *Store) ownsGroup(ctx context.Context, userID, id int64) (bool, error) {
	return s.rowExists(ctx, "category_groups", userID, id)
}

// rowExists checks a scoped id against a fixed set of trusted table names.
// The table name is never user-controlled (always a package-internal literal),
// so string concatenation here is safe.
func (s *Store) rowExists(ctx context.Context, table string, userID, id int64) (bool, error) {
	// COUNT(*) yields an integer that Scans into *int. (Postgres SELECT EXISTS(...)
	// returns boolean, which will not Scan into *int.)
	var n int
	if err := s.queryOne(ctx,
		`SELECT COUNT(*) FROM `+table+` WHERE id=$1 AND user_id=$2`, id, userID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// requireCategory / requireAccount / requireGroup return ErrNotOwned when the
// user-supplied foreign-key id is not owned by userID.
func (s *Store) requireCategory(ctx context.Context, userID, id int64) error {
	ok, err := s.ownsCategory(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotOwned
	}
	return nil
}

func (s *Store) requireAccount(ctx context.Context, userID, id int64) error {
	ok, err := s.ownsAccount(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotOwned
	}
	return nil
}

func (s *Store) requireGroup(ctx context.Context, userID, id int64) error {
	ok, err := s.ownsGroup(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotOwned
	}
	return nil
}

// nullTime scans NULL DATETIME into *time.Time.
type nullTime struct{ sql.NullTime }

func (n nullTime) Ptr() *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

func nullInt(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullStr(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}
