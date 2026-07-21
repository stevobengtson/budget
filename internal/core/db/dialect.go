package db

// Dialect identifies which SQL dialect the underlying *sql.DB speaks. The app
// targets Postgres only; the type is retained (Postgres-only) so the
// admin/migration helpers keep a stable signature.
type Dialect int

const (
	DialectPostgres Dialect = iota
)

func (d Dialect) String() string { return "postgres" }
