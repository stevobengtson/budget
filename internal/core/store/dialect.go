package store

import (
	"context"
	"database/sql"
	"fmt"
)

// monthExpr returns a Postgres SQL expression that yields a "YYYY-MM" string
// from the named DATE/TIMESTAMP column.
func monthExpr(col string) string {
	return fmt.Sprintf("to_char(%s, 'YYYY-MM')", col)
}

// run/queryAll/queryOne are thin wrappers over the underlying *sql.DB. All
// store SQL uses native Postgres $1,$2,... placeholders.
func (s *Store) run(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

func (s *Store) queryAll(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

func (s *Store) queryOne(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// insertReturningID runs an INSERT with " RETURNING id" appended and scans the
// generated row id.
func (s *Store) insertReturningID(ctx context.Context, query string, args ...any) (int64, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// txExec/txQueryOne wrap *sql.Tx calls.
func (s *Store) txExec(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return tx.ExecContext(ctx, query, args...)
}

func (s *Store) txQueryOne(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	return tx.QueryRowContext(ctx, query, args...)
}

// txInsertReturningID is the *sql.Tx counterpart to insertReturningID.
func (s *Store) txInsertReturningID(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	var id int64
	if err := tx.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}
