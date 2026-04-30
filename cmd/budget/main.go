package main

import (
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
	var dbPath string
	flag.StringVar(&dbPath, "db", "", "path to SQLite database (default: $XDG_CONFIG_HOME/budget/budget.db)")
	flag.Parse()

	if dbPath == "" {
		p, err := db.DefaultPath()
		if err != nil {
			fail(err)
		}
		dbPath = p
	}

	conn, err := db.Open(dbPath)
	if err != nil {
		fail(err)
	}
	defer func() { _ = conn.Close() }()

	zone.NewGlobal()

	s := store.New(conn)
	m := tui.New(s)
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "budget:", err)
	os.Exit(1)
}
