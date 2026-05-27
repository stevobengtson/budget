package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Dialect identifies which SQL dialect the underlying *sql.DB speaks.
type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

func (d Dialect) String() string {
	switch d {
	case DialectPostgres:
		return "postgres"
	}
	return "sqlite"
}

// Rebind translates a `?`-style query to the dialect's placeholder format.
// SQLite passes through; Postgres rewrites to $1, $2, ... in order.
func (d Dialect) Rebind(query string) string {
	if d != DialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	inSingle := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' {
			inSingle = !inSingle
		}
		if c == '?' && !inSingle {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// MonthExpr returns a dialect-appropriate SQL expression that yields a
// "YYYY-MM" string from the named DATE column.
func (d Dialect) MonthExpr(col string) string {
	if d == DialectPostgres {
		return fmt.Sprintf("to_char(%s, 'YYYY-MM')", col)
	}
	return fmt.Sprintf("strftime('%%Y-%%m', %s)", col)
}

// run/queryAll/queryOne are thin wrappers that apply Rebind before
// delegating to the underlying *sql.DB.
func (s *Store) run(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.dialect.Rebind(query), args...)
}

func (s *Store) queryAll(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.dialect.Rebind(query), args...)
}

func (s *Store) queryOne(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.dialect.Rebind(query), args...)
}

// insertReturningID runs an INSERT and returns the generated row id.
// On SQLite uses LastInsertId(); on Postgres appends "RETURNING id" and
// scans the value.
func (s *Store) insertReturningID(ctx context.Context, query string, args ...any) (int64, error) {
	if s.dialect == DialectPostgres {
		var id int64
		q := s.dialect.Rebind(query + " RETURNING id")
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// txExec/txQueryOne/txQueryAll wrap *sql.Tx calls with Rebind.
func (s *Store) txExec(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return tx.ExecContext(ctx, s.dialect.Rebind(query), args...)
}

func (s *Store) txQueryOne(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	return tx.QueryRowContext(ctx, s.dialect.Rebind(query), args...)
}

// txInsertReturningID is the *sql.Tx counterpart to insertReturningID.
func (s *Store) txInsertReturningID(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	if s.dialect == DialectPostgres {
		var id int64
		q := s.dialect.Rebind(query + " RETURNING id")
		if err := tx.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
