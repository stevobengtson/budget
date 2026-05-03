package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/db"
	"github.com/sbengtson/budget/internal/store"
	"github.com/sbengtson/budget/internal/tui"
)

func main() {
	var (
		dbPath      string
		migrateFrom string
		migrateTo   string
	)
	flag.StringVar(&dbPath, "db", "",
		"database DSN. SQLite path (default: $XDG_CONFIG_HOME/budget/budget.db) or postgres://user:pass@host:5432/dbname")
	flag.StringVar(&migrateFrom, "migrate-from", "",
		"copy all data from this DSN into --migrate-to and exit")
	flag.StringVar(&migrateTo, "migrate-to", "",
		"destination DSN for --migrate-from")
	flag.Parse()

	if migrateFrom != "" || migrateTo != "" {
		if migrateFrom == "" || migrateTo == "" {
			fail(fmt.Errorf("both --migrate-from and --migrate-to are required"))
		}
		if err := runMigrate(migrateFrom, migrateTo); err != nil {
			fail(err)
		}
		return
	}

	if dbPath == "" {
		p, err := db.DefaultPath()
		if err != nil {
			fail(err)
		}
		dbPath = p
	}

	conn, dialect, err := db.Open(dbPath)
	if err != nil {
		fail(err)
	}
	defer func() { _ = conn.Close() }()

	zone.NewGlobal()

	sd := store.DialectSQLite
	if dialect == db.DialectPostgres {
		sd = store.DialectPostgres
	}
	s := store.NewWithDialect(conn, sd)
	m := tui.New(s)
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fail(err)
	}
}

func runMigrate(from, to string) error {
	ctx := context.Background()

	src, srcDialect, err := db.Open(from)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, dstDialect, err := db.Open(to)
	if err != nil {
		return fmt.Errorf("open dest: %w", err)
	}
	defer func() { _ = dst.Close() }()

	fmt.Fprintf(os.Stderr, "migrating data from %s -> %s ...\n", srcDialect, dstDialect)
	if err := db.CopyAll(ctx, src, srcDialect, dst, dstDialect); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "done.")
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "budget:", err)
	os.Exit(1)
}
