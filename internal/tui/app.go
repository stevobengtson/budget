// Package tui implements the terminal UI for the budget app.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/store"
)

type tab int

const (
	tabBudget tab = iota
	tabTx
	tabAccounts
	tabCategories
	tabPaydown
	tabReports
	tabCount
)

func (t tab) Name() string {
	switch t {
	case tabBudget:
		return "Budget"
	case tabTx:
		return "Transactions"
	case tabAccounts:
		return "Accounts"
	case tabCategories:
		return "Categories"
	case tabPaydown:
		return "Paydown"
	case tabReports:
		return "Reports"
	}
	return ""
}

func (t tab) Icon() string {
	switch t {
	case tabBudget:
		return "💰"
	case tabTx:
		return "📒"
	case tabAccounts:
		return "🏦"
	case tabCategories:
		return "🗂"
	case tabPaydown:
		return "📉"
	case tabReports:
		return "📊"
	}
	return ""
}

func tabZoneID(t tab) string { return fmt.Sprintf("tab-%d", t) }

// Model is the root tea.Model.
type Model struct {
	store *store.Store

	active tab
	width  int
	height int

	flash    string
	flashErr bool

	spinner spinner.Model
	busy    bool

	showHelp bool

	accounts     accountsModel
	categories   categoriesModel
	transactions txModel
	budget       budgetModel
	paydown      paydownModel
	reports      reportsModel
}

func New(s *store.Store) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)

	m := Model{
		store:        s,
		active:       tabBudget,
		spinner:      sp,
		accounts:     newAccountsModel(s),
		categories:   newCategoriesModel(s),
		transactions: newTxModel(s),
		budget:       newBudgetModel(s),
		paydown:      newPaydownModel(s),
		reports:      newReportsModel(s),
	}
	// Refresh receivers mutate via pointer; do it on initialization so the
	// first-paint screen has data instead of an empty list.
	_ = m.accounts.Refresh()
	_ = m.categories.Refresh()
	_ = m.transactions.Refresh()
	_ = m.budget.Refresh()
	_ = m.paydown.Refresh()
	_ = m.reports.Refresh()
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) childKeysOnly() bool {
	switch m.active {
	case tabAccounts:
		return m.accounts.modal()
	case tabCategories:
		return m.categories.modal()
	case tabTx:
		return m.transactions.modal()
	case tabBudget:
		return m.budget.modal()
	case tabPaydown:
		return m.paydown.modal()
	}
	return false
}

func (m *Model) routeMouse(msg tea.MouseMsg) tea.Cmd {
	switch m.active {
	case tabAccounts:
		return m.accounts.HandleMouse(msg)
	case tabTx:
		return m.transactions.HandleMouse(msg)
	case tabBudget:
		return m.budget.HandleMouse(msg)
	case tabCategories:
		return m.categories.HandleMouse(msg)
	case tabPaydown:
		return m.paydown.HandleMouse(msg)
	}
	return nil
}

type clearBusyMsg struct{}

func (m *Model) refreshActive() tea.Cmd {
	switch m.active {
	case tabBudget:
		return m.budget.Refresh()
	case tabTx:
		return m.transactions.Refresh()
	case tabAccounts:
		return m.accounts.Refresh()
	case tabCategories:
		return m.categories.Refresh()
	case tabPaydown:
		return m.paydown.Refresh()
	case tabReports:
		return m.reports.Refresh()
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.reports.SetSize(msg.Width, msg.Height)
		m.transactions.SetSize(msg.Width, msg.Height)
		m.budget.SetSize(msg.Width, msg.Height)
		m.budget.adjustScroll()
		m.categories.SetSize(msg.Width, msg.Height)
		m.paydown.SetSize(msg.Width, msg.Height)
	case tea.KeyMsg:
		// Help popup intercepts keys ahead of everything else when open.
		if m.showHelp {
			switch msg.String() {
			case "?", "esc", "q", "ctrl+c":
				m.showHelp = false
				return m, nil
			}
			return m, nil
		}
		if !m.childKeysOnly() {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "?":
				m.showHelp = true
				return m, nil
			case "1":
				m.active = tabBudget
				return m, m.refreshActive()
			case "2":
				m.active = tabTx
				return m, m.refreshActive()
			case "3":
				m.active = tabAccounts
				return m, m.refreshActive()
			case "4":
				m.active = tabCategories
				return m, m.refreshActive()
			case "5":
				m.active = tabPaydown
				return m, m.refreshActive()
			case "6":
				m.active = tabReports
				return m, m.refreshActive()
			case "H", "shift+left":
				m.active = (m.active - 1 + tabCount) % tabCount
				return m, m.refreshActive()
			case "L", "shift+right":
				m.active = (m.active + 1) % tabCount
				return m, m.refreshActive()
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			for i := tab(0); i < tabCount; i++ {
				if zone.Get(tabZoneID(i)).InBounds(msg) {
					m.active = i
					return m, m.refreshActive()
				}
			}
			// fall through: row clicks belong to active screen.
			return m, m.routeMouse(msg)
		}
		// Ignore motion / scroll events.
		return m, nil
	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var c tea.Cmd
		m.spinner, c = m.spinner.Update(msg)
		return m, c
	case clearBusyMsg:
		m.busy = false
		return m, nil
	case flashMsg:
		m.flash = msg.text
		m.flashErr = msg.isErr
		m.busy = true
		return m, tea.Batch(
			m.spinner.Tick,
			tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return clearBusyMsg{} }),
		)
	}

	var cmd tea.Cmd
	switch m.active {
	case tabAccounts:
		m.accounts, cmd = m.accounts.Update(msg)
	case tabCategories:
		m.categories, cmd = m.categories.Update(msg)
	case tabTx:
		m.transactions, cmd = m.transactions.Update(msg)
	case tabBudget:
		m.budget, cmd = m.budget.Update(msg)
	case tabPaydown:
		m.paydown, cmd = m.paydown.Update(msg)
	case tabReports:
		m.reports, cmd = m.reports.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 100
	}

	bar := m.renderTabBar(width)

	var body string
	switch m.active {
	case tabBudget:
		body = m.budget.View()
	case tabTx:
		body = m.transactions.View()
	case tabAccounts:
		body = m.accounts.View()
	case tabCategories:
		body = m.categories.View()
	case tabPaydown:
		body = m.paydown.View()
	case tabReports:
		body = m.reports.View()
	}
	if m.showHelp {
		body = helpView()
	}
	bodyRendered := styleApp.Render(body)

	status := m.renderStatusBar(width)

	height := m.height
	if height <= 0 {
		out := lipgloss.JoinVertical(lipgloss.Left, bar, bodyRendered, status)
		return zone.Scan(out)
	}

	// Pad the body to fill the gap between tab bar and status bar so the
	// status bar pins to the bottom of the terminal.
	contentH := height - lipgloss.Height(bar) - lipgloss.Height(status)
	if contentH < 1 {
		contentH = 1
	}
	body = lipgloss.NewStyle().
		Width(width).
		Height(contentH).
		Render(bodyRendered)

	out := lipgloss.JoinVertical(lipgloss.Left, bar, body, status)
	return zone.Scan(out)
}

func (m Model) renderTabBar(width int) string {
	rendered := make([]string, 0, int(tabCount))
	for i := tab(0); i < tabCount; i++ {
		label := fmt.Sprintf("%d %s %s", i+1, i.Icon(), i.Name())
		var s string
		if i == m.active {
			s = styleTabActive.Render(label)
		} else {
			s = styleTab.Render(label)
		}
		rendered = append(rendered, zone.Mark(tabZoneID(i), s))
	}
	row := lipgloss.JoinHorizontal(lipgloss.Bottom, rendered...)
	gapWidth := width - lipgloss.Width(row) - 2 // 2 for outer app padding
	if gapWidth < 0 {
		gapWidth = 0
	}
	gap := styleTabGap.Render(strings.Repeat(" ", gapWidth))
	return styleApp.Render(lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap))
}

func (m Model) renderStatusBar(width int) string {
	mode := styleStatusMode.Render(strings.ToUpper(m.active.Name()))
	keys := styleStatusKeys.Render(statusHints(m.active))

	right := ""
	if m.flash != "" {
		if m.flashErr {
			right = styleStatusErr.Render("⚠ " + m.flash)
		} else {
			right = styleStatusOK.Render("✓ " + m.flash)
		}
	}

	if m.busy {
		spin := styleStatusBar.Render(" " + m.spinner.View() + " ")
		keys = spin + keys
	}

	left := mode + keys
	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}
	filler := styleStatusBar.Render(strings.Repeat(" ", pad))
	return styleStatusBar.Width(width).Render(left + filler + right)
}

func statusHints(t tab) string {
	switch t {
	case tabBudget:
		return "↑↓ move · enter assign · g goal · i income · </> month · t today · ? help"
	case tabTx:
		return "↑↓ move · n new · enter edit · d delete · c cleared · f acct · </> month · t today · M all · ? help"
	case tabAccounts:
		return "↑↓ move · n new · enter edit · d archive · ? help"
	case tabCategories:
		return "↑↓ move · n new · enter edit · d delete/archive · ? help"
	case tabPaydown:
		return "↑↓ move · a add · e payment · c category · r remove · +/- horizon · ,/. page · ? help"
	case tabReports:
		return "s spending · c cashflow · [/] period · pgup/pgdn page · r refresh · ? help"
	}
	return ""
}

func helpView() string {
	rows := [][2]string{
		{"?", "show / hide this help"},
		{"esc", "close help · cancel form / modal"},
		{"1–6 / click", "switch tabs"},
		{"shift+h / shift+l", "prev / next tab"},
		{"click row", "select row in any list"},
		{"q / ctrl+c", "quit"},
		{"n", "new (in list views)"},
		{"enter", "edit selected · or assign on Budget · or save form"},
		{"d", "delete or archive (with confirm)"},
		{"g", "(budget) set goal & due date"},
		{"i", "(budget) manage income for the month"},
		{"</>", "(budget) prev / next month"},
		{"space", "open picker on Type / Account / Category / Date"},
		{"f / F", "(transactions) filter by account / clear filter"},
		{"< / >", "(transactions) prev / next month filter"},
		{"t / M", "(transactions) jump to current month / clear month filter"},
		{"a / e / r", "(paydown) add account · edit payment · remove"},
		{"c", "(paydown) link payment category for selected account"},
		{"+ / -", "(paydown) extend / shrink horizon (12-month steps)"},
		{", / .", "(paydown) page back / forward (also pgup/pgdn)"},
		{"s / c", "(reports) spending by category · monthly cashflow"},
		{"[ / ]", "(reports) prev / next spending period"},
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Help · keymap"))
	b.WriteString("\n\n")
	for _, r := range rows {
		b.WriteString(styleSelected.Render(padRight(r[0], 14)))
		b.WriteString(r[1])
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render("Amounts: enter dollars (e.g. 12.50). Outflow & inflow are mutually exclusive."))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("Mouse: click tabs to switch. Use arrows / vim keys inside views."))
	return stylePanel.Render(b.String())
}

type flashMsg struct {
	text  string
	isErr bool
}

func flashOK(s string) tea.Cmd   { return func() tea.Msg { return flashMsg{text: s} } }
func flashFail(s string) tea.Cmd { return func() tea.Msg { return flashMsg{text: s, isErr: true} } }
