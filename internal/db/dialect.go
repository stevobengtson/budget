package db

// Dialect identifies which SQL dialect the underlying *sql.DB speaks.
// Defined here (rather than in store) to avoid an import cycle, since
// store_test.go imports the db package for opening test connections.
type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

func (d Dialect) String() string {
	if d == DialectPostgres {
		return "postgres"
	}
	return "sqlite"
}
