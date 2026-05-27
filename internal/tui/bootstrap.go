package tui

import (
	"database/sql"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sbengtson/budget/internal/core/db"
	"github.com/sbengtson/budget/internal/core/store"
)

// DefaultConnectTimeout is the deadline applied to the initial database
// ping when the TUI bootstraps. Migrations may still take longer; this
// only bounds reachability.
const DefaultConnectTimeout = 5 * time.Second

// Bootstrap is the root tea.Model when the TUI is responsible for opening
// the database itself. It shows a loading screen while the connection is
// being established, swaps in the full Model on success, and renders an
// error screen if the open fails (or the user can't reach Postgres). The
// caller is responsible for invoking Close() after the bubbletea program
// exits so the underlying *sql.DB is released.
type Bootstrap struct {
	dsn     string
	timeout time.Duration

	spinner spinner.Model
	err     error
	ready   *Model
	conn    *sql.DB

	width, height int
}

// NewBootstrap returns a Bootstrap that will open dsn when Init() runs.
// Pass 0 for timeout to use DefaultConnectTimeout.
func NewBootstrap(dsn string, timeout time.Duration) *Bootstrap {
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)
	return &Bootstrap{dsn: dsn, timeout: timeout, spinner: sp}
}

// Close releases the database connection if one was successfully opened.
func (b *Bootstrap) Close() {
	if b.conn != nil {
		_ = b.conn.Close()
		b.conn = nil
	}
}

type dbReadyMsg struct {
	conn    *sql.DB
	dialect db.Dialect
}

type dbErrMsg struct{ err error }

func (b *Bootstrap) Init() tea.Cmd {
	return tea.Batch(b.spinner.Tick, b.connectCmd())
}

func (b *Bootstrap) connectCmd() tea.Cmd {
	dsn := b.dsn
	timeout := b.timeout
	return func() tea.Msg {
		conn, dialect, err := db.OpenWithTimeout(dsn, timeout)
		if err != nil {
			return dbErrMsg{err: err}
		}
		return dbReadyMsg{conn: conn, dialect: dialect}
	}
}

func (b *Bootstrap) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
		if b.ready != nil {
			return b.delegate(msg)
		}
		return b, nil

	case dbReadyMsg:
		sd := store.DialectSQLite
		if msg.dialect == db.DialectPostgres {
			sd = store.DialectPostgres
		}
		s := store.NewWithDialect(msg.conn, sd)
		m := New(s)
		b.conn = msg.conn
		b.ready = &m
		// Forward the current window size so the new model lays out
		// correctly on first paint.
		if b.width > 0 || b.height > 0 {
			updated, _ := b.ready.Update(tea.WindowSizeMsg{Width: b.width, Height: b.height})
			mm := updated.(Model)
			b.ready = &mm
		}
		return b, b.ready.Init()

	case dbErrMsg:
		b.err = msg.err
		return b, nil

	case spinner.TickMsg:
		// Once the inner model owns its own spinner, route ticks there.
		if b.ready != nil {
			return b.delegate(msg)
		}
		// Errored — stop animating.
		if b.err != nil {
			return b, nil
		}
		var c tea.Cmd
		b.spinner, c = b.spinner.Update(msg)
		return b, c

	case tea.KeyMsg:
		if b.err != nil {
			switch msg.String() {
			case "q", "esc", "ctrl+c":
				return b, tea.Quit
			}
			return b, nil
		}
		if b.ready != nil {
			return b.delegate(msg)
		}
		// Loading screen — let the user bail out.
		switch msg.String() {
		case "q", "ctrl+c":
			return b, tea.Quit
		}
		return b, nil
	}

	if b.ready != nil {
		return b.delegate(msg)
	}
	return b, nil
}

func (b *Bootstrap) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := b.ready.Update(msg)
	mm := updated.(Model)
	b.ready = &mm
	return b, cmd
}

func (b *Bootstrap) View() string {
	if b.err != nil {
		return b.errorView()
	}
	if b.ready != nil {
		return b.ready.View()
	}
	return b.loadingView()
}

func (b *Bootstrap) loadingView() string {
	title := styleTitle.Render("💰 budget")
	line := lipgloss.JoinHorizontal(lipgloss.Center,
		b.spinner.View(), " ", "Loading budget…")
	hint := styleDim.Render("connecting to " + b.shortDSN() + " (press q to cancel)")

	body := lipgloss.JoinVertical(lipgloss.Center, title, "", line, "", hint)
	return b.centered(body)
}

func (b *Bootstrap) errorView() string {
	title := styleErr.Render("⚠ Failed to load budget")
	detail := lipgloss.NewStyle().Foreground(colorBad).Render(b.err.Error())
	hint := styleDim.Render("Press q to quit. Check your DSN in budget.yaml or BUDGET_DB_DSN.")

	body := lipgloss.JoinVertical(lipgloss.Left, title, "", detail, "", hint)
	return b.centered(stylePanel.Render(body))
}

// centered places content in the middle of the available terminal area.
// Falls back to a left-aligned render if no size is known yet.
func (b *Bootstrap) centered(content string) string {
	if b.width <= 0 || b.height <= 0 {
		return styleApp.Render(content)
	}
	box := lipgloss.NewStyle().
		Width(b.width).
		Height(b.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
	return box
}

// shortDSN trims credentials and overly long file paths from the DSN for
// display so the loading screen doesn't leak passwords or wrap awkwardly.
func (b *Bootstrap) shortDSN() string {
	d := b.dsn
	if len(d) == 0 {
		return "(default)"
	}
	// Postgres URLs may carry user:password@host; strip credentials.
	if i := strings.Index(d, "@"); i > 0 {
		if j := strings.Index(d[:i], "//"); j >= 0 {
			d = d[:j+2] + "***@" + d[i+1:]
		}
	}
	const max = 60
	if len(d) > max {
		return d[:max-1] + "…"
	}
	return d
}

// compileCheck guarantees Bootstrap satisfies tea.Model at build time.
var _ tea.Model = (*Bootstrap)(nil)
