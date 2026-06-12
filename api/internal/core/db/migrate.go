package db

import (
	"context"
	"database/sql"
	"fmt"
)

// CopyAll copies every budget table from src to dst, preserving primary
// keys. Both connections must be opened (with migrations applied) by Open
// already.
//
// The destination is wiped first (all budget tables) so any pre-existing
// data — including the system-seeded Income category from migration
// 00005 — is replaced by an exact copy of the source. Goose's migration
// metadata is left intact.
//
// After insert, Postgres sequences are advanced past the maximum imported id.
func CopyAll(ctx context.Context, src *sql.DB, srcDialect Dialect, dst *sql.DB, dstDialect Dialect) error {
	// Order matters because of foreign keys.
	tables := []string{
		"category_groups",
		"accounts",
		"categories",
		"budgets",
		"transactions",
		"incomes",
	}

	if err := wipeDestination(ctx, dst, dstDialect, tables); err != nil {
		return fmt.Errorf("wipe destination: %w", err)
	}

	dstTx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = dstTx.Rollback() }()

	// Disable triggers (incl. FK constraints) on Postgres while we copy
	// so order doesn't matter for self-referential tables.
	if dstDialect == DialectPostgres {
		for _, t := range tables {
			if _, err := dstTx.ExecContext(ctx, "ALTER TABLE "+t+" DISABLE TRIGGER ALL"); err != nil {
				return fmt.Errorf("disable triggers %s: %w", t, err)
			}
		}
	}

	for _, t := range tables {
		if err := copyTable(ctx, src, srcDialect, dstTx, dstDialect, t); err != nil {
			return fmt.Errorf("copy %s: %w", t, err)
		}
	}

	if dstDialect == DialectPostgres {
		for _, t := range tables {
			if _, err := dstTx.ExecContext(ctx, "ALTER TABLE "+t+" ENABLE TRIGGER ALL"); err != nil {
				return fmt.Errorf("enable triggers %s: %w", t, err)
			}
		}
		// Bump auto-increment sequences past the imported max(id). The
		// budgets table has a compound primary key (no id column) so it
		// has no sequence — skip it.
		seqTables := []string{"category_groups", "accounts", "categories", "transactions", "incomes"}
		for _, t := range seqTables {
			seq := t + "_id_seq"
			q := fmt.Sprintf(
				"SELECT setval('%s', COALESCE((SELECT MAX(id) FROM %s), 1), true)",
				seq, t)
			if _, err := dstTx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("bump sequence %s: %w", seq, err)
			}
		}
	}

	return dstTx.Commit()
}

// wipeDestination removes all rows from the budget tables. On Postgres
// uses TRUNCATE ... CASCADE; on SQLite issues DELETE in reverse-dependency
// order with FK checks suspended for the duration.
func wipeDestination(ctx context.Context, dst *sql.DB, d Dialect, tables []string) error {
	if d == DialectPostgres {
		quoted := make([]string, len(tables))
		for i, t := range tables {
			quoted[i] = t
		}
		_, err := dst.ExecContext(ctx,
			"TRUNCATE TABLE "+joinSep(quoted, ", ")+" RESTART IDENTITY CASCADE")
		return err
	}
	if _, err := dst.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() { _, _ = dst.ExecContext(ctx, "PRAGMA foreign_keys = ON") }()
	for i := len(tables) - 1; i >= 0; i-- {
		if _, err := dst.ExecContext(ctx, "DELETE FROM "+tables[i]); err != nil {
			return err
		}
	}
	// Reset SQLite's internal autoincrement counters too, so re-importing
	// keeps source ids exact.
	_, _ = dst.ExecContext(ctx, "DELETE FROM sqlite_sequence")
	return nil
}

// boolColumns lists the columns whose declared type is BOOLEAN. SQLite
// stores them as 0/1 INTEGERs, so when copying into Postgres we convert
// them back to native bool before inserting.
var boolColumns = map[string]map[string]bool{
	"accounts":     {"include_in_paydown": true},
	"transactions": {"cleared": true},
	"categories":   {"is_income": true},
}

// copyTable streams rows from src and inserts into dstTx with column-aware
// placeholder rewriting and BOOLEAN coercion for Postgres destinations.
func copyTable(ctx context.Context, src *sql.DB, srcDialect Dialect, dstTx *sql.Tx, dstDialect Dialect, table string) error {
	cols, err := tableColumns(ctx, src, srcDialect, table)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if len(cols) == 0 {
		return fmt.Errorf("no columns found for %s", table)
	}

	colList := joinIdent(cols)
	rows, err := src.QueryContext(ctx, "SELECT "+colList+" FROM "+table)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	placeholders := make([]string, len(cols))
	for i := range placeholders {
		if dstDialect == DialectPostgres {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		} else {
			placeholders[i] = "?"
		}
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, colList, joinSep(placeholders, ", "))

	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if dstDialect == DialectPostgres {
			coerceForPostgres(table, cols, raw)
		}
		if _, err := dstTx.ExecContext(ctx, insertSQL, raw...); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return rows.Err()
}

// coerceForPostgres adjusts row values so the pgx driver can encode them.
// SQLite hands back BOOLEAN columns as int64; Postgres needs bool.
func coerceForPostgres(table string, cols []string, raw []any) {
	bools := boolColumns[table]
	for i, name := range cols {
		if !bools[name] {
			continue
		}
		switch v := raw[i].(type) {
		case int64:
			raw[i] = v != 0
		case bool:
			// already bool
		case nil:
			// leave nil
		}
	}
}

// tableColumns returns ordered column names for a table.
func tableColumns(ctx context.Context, conn *sql.DB, d Dialect, table string) ([]string, error) {
	var rows *sql.Rows
	var err error
	switch d {
	case DialectPostgres:
		rows, err = conn.QueryContext(ctx,
			`SELECT column_name FROM information_schema.columns
			 WHERE table_schema = current_schema() AND table_name = $1
			 ORDER BY ordinal_position`, table)
	default:
		rows, err = conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cols []string
	if d == DialectPostgres {
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			cols = append(cols, name)
		}
		return cols, rows.Err()
	}
	// SQLite PRAGMA: cid, name, type, notnull, dflt_value, pk
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func joinIdent(cols []string) string { return joinSep(cols, ", ") }

func joinSep(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
