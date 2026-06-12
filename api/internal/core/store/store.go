// Package store is the persistence layer over the budget database
// (SQLite or Postgres). All SQL is written using `?` placeholders and a
// small dialect helper rebinds them as needed.
package store

import (
	"database/sql"
	"time"
)

type Store struct {
	db      *sql.DB
	dialect Dialect
}

func New(db *sql.DB) *Store { return &Store{db: db, dialect: DialectSQLite} }

func NewWithDialect(db *sql.DB, d Dialect) *Store {
	return &Store{db: db, dialect: d}
}

func (s *Store) DB() *sql.DB      { return s.db }
func (s *Store) Dialect() Dialect { return s.dialect }

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
