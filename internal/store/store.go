// Package store is the persistence layer over the budget SQLite database.
package store

import (
	"database/sql"
	"time"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) DB() *sql.DB { return s.db }

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
