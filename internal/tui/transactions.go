package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	datepicker "github.com/ethanefung/bubble-datepicker"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
)

type txMode int

const (
	txList txMode = iota
	txForm
	txAccountPick
	txCategoryPick
	txConfirmDel
	txFilterPick
	txDatePick
)

type txModel struct {
	store    *store.Store
	rows     []store.Transaction
	accounts []store.AccountWithBalance
	cats     []store.Category
	groups   []store.CategoryGroup

	cursor  int
	mode    txMode
	form    form
	picker  picker
	confirm confirmModel
	editing *store.Transaction

	pickerTarget    int // 1=account, 2=category, 5=transferAccount
	filterAccountID *int64
	filterMonth     string // "YYYY-MM"; empty = all months

	dp    datepicker.Model
	pager paginator.Model

	// pageStarts[i] is the index into rows where page i begins. Pages are
	// sized by rendered line count (transactions + date headers) so a page
	// never overflows the terminal height.
	pageStarts []int

	width, height int
}

func newTxModel(s *store.Store) txModel {
	pg := paginator.New()
	pg.Type = paginator.Arabic
	return txModel{
		store:       s,
		filterMonth: store.MonthKey(time.Now()),
		pager:       pg,
	}
}

func (m *txModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.recomputePagination()
}

// syncPageToCursor flips the page so the cursor remains visible.
func (m *txModel) syncPageToCursor() {
	m.pager.Page = m.pageOf(m.cursor)
}

// pageOf returns the page index whose slice contains the given row index.
func (m *txModel) pageOf(idx int) int {
	p := 0
	for i, s := range m.pageStarts {
		if s <= idx {
			p = i
		} else {
			break
		}
	}
	return p
}

// pageBounds returns the [start, end) row range rendered on the current page.
func (m *txModel) pageBounds() (int, int) {
	if len(m.pageStarts) == 0 {
		return 0, len(m.rows)
	}
	page := m.pager.Page
	if page < 0 {
		page = 0
	}
	if page >= len(m.pageStarts) {
		page = len(m.pageStarts) - 1
	}
	start := m.pageStarts[page]
	end := len(m.rows)
	if page+1 < len(m.pageStarts) {
		end = m.pageStarts[page+1]
	}
	return start, end
}

func (m *txModel) recomputePagination() {
	// Chrome rows: tab bar (3) + title (1) + column header (1) + pager line (1)
	// + help line (1) + status bar (1) + outer padding/blank lines (~3).
	chrome := 11
	budget := m.height - chrome
	if budget < 5 {
		budget = 5
	}
	m.pageStarts = computePageStarts(m.rows, budget)
	m.pager.SetTotalPages(len(m.pageStarts))
	if m.pager.Page >= m.pager.TotalPages {
		m.pager.Page = max0(m.pager.TotalPages - 1)
	}
}

// computePageStarts partitions rows into pages whose rendered height fits
// within budget lines. Each transaction costs one line; the first row of a day
// costs an extra line for its date header. Every page always holds at least
// one transaction.
func computePageStarts(rows []store.Transaction, budget int) []int {
	starts := []int{0}
	if len(rows) == 0 || budget < 1 {
		return starts
	}
	used := 0
	lastDay := ""
	for i := range rows {
		day := rows[i].Date.Format("2006-01-02")
		cost := 1
		if day != lastDay {
			cost++ // date header line
		}
		pageStart := starts[len(starts)-1]
		if used+cost > budget && i != pageStart {
			starts = append(starts, i)
			used = 1 + 1 // new page: this row + its (always-emitted) date header
			lastDay = day
			continue
		}
		used += cost
		lastDay = day
	}
	return starts
}

func (m txModel) modal() bool { return m.mode != txList }

func (m *txModel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != txList {
		return nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	for i := range m.rows {
		if zone.Get("tx-row-" + strconv.Itoa(i)).InBounds(msg) {
			m.cursor = i
			return nil
		}
	}
	return nil
}

func (m txModel) Init() tea.Cmd { return m.Refresh() }

func (m *txModel) Refresh() tea.Cmd {
	ctx := context.Background()
	rows, err := m.store.ListTransactions(ctx, store.TxFilter{
		Limit:     500,
		AccountID: m.filterAccountID,
		Month:     m.filterMonth,
	})
	if err != nil {
		return flashFail("transactions: " + err.Error())
	}
	accs, err := m.store.ListAccounts(ctx, true)
	if err != nil {
		return flashFail("accounts: " + err.Error())
	}
	cats, err := m.store.ListCategories(ctx, true)
	if err != nil {
		return flashFail("categories: " + err.Error())
	}
	groups, _ := m.store.ListGroups(ctx)
	m.rows = rows
	m.accounts = accs
	m.cats = cats
	m.groups = groups
	if m.cursor >= len(m.rows) {
		m.cursor = max0(len(m.rows) - 1)
	}
	m.recomputePagination()
	return nil
}

func (m txModel) Update(msg tea.Msg) (txModel, tea.Cmd) {
	switch m.mode {
	case txList:
		return m.updateList(msg)
	case txForm:
		return m.updateForm(msg)
	case txAccountPick, txCategoryPick:
		return m.updatePicker(msg)
	case txFilterPick:
		return m.updateFilterPicker(msg)
	case txDatePick:
		return m.updateDatePicker(msg)
	case txConfirmDel:
		return m.updateConfirm(msg)
	}
	return m, nil
}

func (m txModel) updateList(msg tea.Msg) (txModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.syncPageToCursor()
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.syncPageToCursor()
			}
		case "pgdown", "pgdn", "ctrl+d":
			m.pager.NextPage()
			m.cursor = m.pageStarts[m.pager.Page]
		case "pgup", "ctrl+u":
			m.pager.PrevPage()
			m.cursor = m.pageStarts[m.pager.Page]
		case "home":
			m.pager.Page = 0
			m.cursor = 0
		case "end":
			m.pager.Page = max0(m.pager.TotalPages - 1)
			m.cursor = max0(len(m.rows) - 1)
		case "n":
			if len(m.accounts) == 0 {
				return m, flashFail("create an account first")
			}
			m.startForm(nil)
		case "enter":
			if len(m.rows) > 0 {
				t := m.rows[m.cursor]
				if t.TransferPairID != nil {
					return m, flashFail("transfers are read-only here; delete and recreate")
				}
				m.startForm(&t)
			}
		case "d":
			if len(m.rows) > 0 {
				m.confirm = confirmModel{prompt: "Delete this transaction?"}
				m.mode = txConfirmDel
			}
		case "f":
			items := []string{"All accounts"}
			cur := 0
			for i, a := range m.accounts {
				items = append(items, a.Name)
				if m.filterAccountID != nil && a.ID == *m.filterAccountID {
					cur = i + 1
				}
			}
			m.picker = newPicker("Filter by account", items, cur)
			m.mode = txFilterPick
			return m, nil
		case "F":
			m.filterAccountID = nil
			return m, m.Refresh()
		case "h", "<", ",":
			if m.filterMonth == "" {
				m.filterMonth = store.MonthKey(time.Now())
			} else {
				m.filterMonth = store.PrevMonth(m.filterMonth)
			}
			m.cursor = 0
			return m, m.Refresh()
		case "l", ">", ".":
			if m.filterMonth == "" {
				m.filterMonth = store.MonthKey(time.Now())
			} else {
				t, _ := time.Parse("2006-01", m.filterMonth)
				m.filterMonth = t.AddDate(0, 1, 0).Format("2006-01")
			}
			m.cursor = 0
			return m, m.Refresh()
		case "t":
			m.filterMonth = store.MonthKey(time.Now())
			m.cursor = 0
			return m, m.Refresh()
		case "M":
			m.filterMonth = ""
			m.cursor = 0
			return m, m.Refresh()
		case "c":
			// toggle cleared on selected
			if len(m.rows) > 0 {
				t := m.rows[m.cursor]
				if t.TransferPairID == nil {
					t.Cleared = !t.Cleared
					if err := m.store.UpdateTransaction(context.Background(), t); err != nil {
						return m, flashFail(err.Error())
					}
					return m, m.Refresh()
				}
			}
		}
	}
	return m, nil
}

func (m *txModel) startForm(existing *store.Transaction) {
	m.editing = existing

	defaultAcct := ""
	if len(m.accounts) > 0 {
		defaultAcct = m.accounts[0].Name
	}
	defaultCat := ""
	if len(m.cats) > 0 {
		defaultCat = m.cats[0].Name
	}

	fields := []field{
		newField("Date", time.Now().Format("2006-01-02"), "YYYY-MM-DD · space opens picker"),
		newField("Account", defaultAcct, "press space to pick"),
		newField("Category", defaultCat, "space=pick · pair with transfer-to for CC payments"),
		newField("Payee", "", ""),
		newField("Notes", "", ""),
		newField("Outflow", "", "dollars; leave blank if inflow"),
		newField("Inflow", "", "dollars; leave blank if outflow"),
		newField("Transfer to (optional)", "", "account name; mutually excl. with category"),
	}
	m.form = form{fields: fields}
	m.form.fields[0].input.SetValue(time.Now().Format("2006-01-02"))
	m.form.fields[1].input.SetValue(defaultAcct)
	m.form.fields[2].input.SetValue(defaultCat)

	if existing != nil {
		acctName := lookupAccount(m.accounts, existing.AccountID)
		catName := ""
		if existing.CategoryID != nil {
			catName = lookupCategory(m.cats, *existing.CategoryID)
		}
		out := ""
		if existing.OutflowCents > 0 {
			out = money.Format(existing.OutflowCents)
		}
		in := ""
		if existing.InflowCents > 0 {
			in = money.Format(existing.InflowCents)
		}
		payee := ""
		if existing.Payee != nil {
			payee = *existing.Payee
		}
		notes := ""
		if existing.Notes != nil {
			notes = *existing.Notes
		}
		m.form.SetValues([]string{
			existing.Date.Format("2006-01-02"),
			acctName, catName, payee, notes, out, in, "",
		})
	}
	m.form.Focus()
	m.mode = txForm
}

func lookupAccount(accs []store.AccountWithBalance, id int64) string {
	for _, a := range accs {
		if a.ID == id {
			return a.Name
		}
	}
	return ""
}

func lookupCategory(cats []store.Category, id int64) string {
	for _, c := range cats {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

func (m txModel) updateForm(msg tea.Msg) (txModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == " " || km.String() == "ctrl+t" {
			switch m.form.focus {
			case 0:
				m.openDatePicker()
				return m, nil
			case 1:
				m.openAccountPicker(1)
				return m, nil
			case 2:
				m.openCategoryPicker()
				return m, nil
			case 7:
				m.openAccountPicker(7)
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = txList
		return m, cmd
	}
	if m.form.submitted {
		return m.save()
	}
	return m, cmd
}

func (m *txModel) openDatePicker() {
	t, err := time.Parse("2006-01-02", m.form.fields[0].input.Value())
	if err != nil {
		t = time.Now()
	}
	dp := datepicker.New(t)
	dp.SetFocus(datepicker.FocusCalendar)
	// Datepicker only renders the focused/selected styles when Selected is
	// true. Without this the highlight is never drawn.
	dp.SelectDate()
	// Disable the picker's built-in `q` quit binding — `q` would otherwise
	// terminate Bubble Tea while editing a transaction.
	dp.KeyMap.Quit = key.NewBinding(key.WithDisabled())
	dp.Styles = datepickerStyles()
	m.dp = dp
	m.mode = txDatePick
}

// datepickerStyles produces a high-contrast theme so the focused day stands
// out clearly against the panel background.
func datepickerStyles() datepicker.Styles {
	s := datepicker.DefaultStyles()
	s.Header = lipgloss.NewStyle().Padding(0, 0, 1, 0)
	s.HeaderText = lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true)
	s.Date = lipgloss.NewStyle().Padding(0, 1)
	s.Text = lipgloss.NewStyle().Foreground(colorMuted)
	// Cursor day: filled accent background — unmissable.
	s.FocusedText = lipgloss.NewStyle().
		Background(colorAccent).
		Foreground(colorOnDark).
		Bold(true)
	// Non-focused calendar focus state (e.g. when month/year focused) still
	// shows the underlying selected day distinctly.
	s.SelectedText = lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Underline(true)
	return s
}

func (m txModel) updateDatePicker(msg tea.Msg) (txModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.mode = txForm
			return m, nil
		case "enter":
			m.form.fields[0].input.SetValue(m.dp.Time.Format("2006-01-02"))
			m.mode = txForm
			return m, nil
		case "t":
			m.dp.SetTime(time.Now())
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.dp, cmd = m.dp.Update(msg)
	return m, cmd
}

func (m *txModel) openAccountPicker(target int) {
	items := make([]string, len(m.accounts))
	cur := 0
	currentName := m.form.fields[target].input.Value()
	for i, a := range m.accounts {
		items[i] = a.Name
		if a.Name == currentName {
			cur = i
		}
	}
	m.pickerTarget = target
	m.picker = newPicker("Account", items, cur)
	m.mode = txAccountPick
}

func (m *txModel) openCategoryPicker() {
	items := []string{"(none / transfer)"}
	cur := 0
	currentName := m.form.fields[2].input.Value()
	for _, g := range m.groups {
		for _, c := range m.cats {
			if c.GroupID == g.ID {
				label := g.Name + " · " + c.Name
				if c.Name == currentName {
					cur = len(items)
				}
				items = append(items, label)
			}
		}
	}
	m.pickerTarget = 2
	m.picker = newPicker("Category", items, cur)
	m.mode = txCategoryPick
}

func (m txModel) updatePicker(msg tea.Msg) (txModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = txForm
		return m, cmd
	}
	if m.picker.chosen {
		choice := m.picker.items[m.picker.cursor]
		switch m.pickerTarget {
		case 1, 7:
			m.form.fields[m.pickerTarget].input.SetValue(choice)
		case 2:
			if m.picker.cursor == 0 {
				m.form.fields[2].input.SetValue("")
			} else {
				m.form.fields[2].input.SetValue(stripGroupPrefix(choice))
			}
		}
		m.mode = txForm
	}
	return m, cmd
}

func (m txModel) save() (txModel, tea.Cmd) {
	vals := m.form.Values()
	date, err := time.Parse("2006-01-02", vals[0])
	if err != nil {
		m.form.err = "date: " + err.Error()
		return m, nil
	}
	var acctID int64
	for _, a := range m.accounts {
		if a.Name == vals[1] {
			acctID = a.ID
			break
		}
	}
	if acctID == 0 {
		m.form.err = "account required"
		return m, nil
	}

	transferTo := vals[7]
	hasCategory := vals[2] != ""
	hasTransfer := transferTo != ""

	var outCents, inCents int64
	if vals[5] != "" {
		c, err := money.Parse(vals[5])
		if err != nil || c < 0 {
			m.form.err = "outflow: positive number"
			return m, nil
		}
		outCents = c
	}
	if vals[6] != "" {
		c, err := money.Parse(vals[6])
		if err != nil || c < 0 {
			m.form.err = "inflow: positive number"
			return m, nil
		}
		inCents = c
	}
	if outCents > 0 && inCents > 0 {
		m.form.err = "set outflow OR inflow, not both"
		return m, nil
	}
	if outCents == 0 && inCents == 0 {
		m.form.err = "amount required"
		return m, nil
	}

	ctx := context.Background()

	if hasTransfer {
		var toID int64
		for _, a := range m.accounts {
			if a.Name == transferTo {
				toID = a.ID
				break
			}
		}
		if toID == 0 {
			m.form.err = "transfer-to: not an account"
			return m, nil
		}
		amount := outCents + inCents
		// outflow → from this account, inflow → to this account.
		var fromID, toAcct int64
		if outCents > 0 {
			fromID, toAcct = acctID, toID
		} else {
			fromID, toAcct = toID, acctID
		}
		var notesPtr *string
		if vals[4] != "" {
			n := vals[4]
			notesPtr = &n
		}
		// Category, if supplied, attaches to the from-leg so the budget
		// envelope reflects this payment. Common case: paying down a credit
		// card from checking against a "CC Payment" category.
		var catPtr *int64
		if hasCategory {
			for _, c := range m.cats {
				if c.Name == vals[2] {
					cid := c.ID
					catPtr = &cid
					break
				}
			}
			if catPtr == nil {
				m.form.err = "category not found"
				return m, nil
			}
		}
		if _, _, err := m.store.CreateTransfer(ctx, store.TransferInput{
			Date: date, FromAccountID: fromID, ToAccountID: toAcct, AmountCents: amount,
			CategoryID: catPtr, Notes: notesPtr,
		}); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		m.mode = txList
		return m, tea.Batch(m.Refresh(), flashOK("Transfer saved"))
	}

	var catPtr *int64
	if hasCategory {
		for _, c := range m.cats {
			if c.Name == vals[2] {
				cid := c.ID
				catPtr = &cid
				break
			}
		}
		if catPtr == nil {
			m.form.err = "category not found"
			return m, nil
		}
	}

	var payeePtr, notesPtr *string
	if vals[3] != "" {
		p := vals[3]
		payeePtr = &p
	}
	if vals[4] != "" {
		n := vals[4]
		notesPtr = &n
	}

	t := store.Transaction{
		Date:         date,
		AccountID:    acctID,
		CategoryID:   catPtr,
		Payee:        payeePtr,
		Notes:        notesPtr,
		OutflowCents: outCents,
		InflowCents:  inCents,
	}
	if m.editing != nil {
		t.ID = m.editing.ID
		t.Cleared = m.editing.Cleared
		if err := m.store.UpdateTransaction(ctx, t); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
	} else {
		if _, err := m.store.CreateTransaction(ctx, t); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
	}
	m.mode = txList
	return m, tea.Batch(m.Refresh(), flashOK("Saved"))
}

func (m txModel) updateFilterPicker(msg tea.Msg) (txModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = txList
		return m, cmd
	}
	if m.picker.chosen {
		if m.picker.cursor == 0 {
			m.filterAccountID = nil
		} else {
			id := m.accounts[m.picker.cursor-1].ID
			m.filterAccountID = &id
		}
		m.cursor = 0
		m.mode = txList
		return m, tea.Batch(cmd, m.Refresh())
	}
	return m, cmd
}

func (m txModel) updateConfirm(msg tea.Msg) (txModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	if m.confirm.answered {
		if m.confirm.yes && len(m.rows) > 0 {
			id := m.rows[m.cursor].ID
			if err := m.store.DeleteTransaction(context.Background(), id); err != nil {
				cmd = tea.Batch(cmd, flashFail(err.Error()))
			} else {
				cmd = tea.Batch(cmd, flashOK("Deleted"))
			}
			cmd = tea.Batch(cmd, m.Refresh())
		}
		m.mode = txList
	}
	return m, cmd
}

func (m txModel) View() string {
	switch m.mode {
	case txForm:
		title := "New transaction"
		if m.editing != nil {
			title = "Edit transaction"
		}
		return m.form.View(title)
	case txAccountPick, txCategoryPick, txFilterPick:
		return m.picker.View()
	case txDatePick:
		return m.viewDatePicker()
	case txConfirmDel:
		return m.confirm.View()
	}
	return m.viewList()
}

func (m txModel) viewDatePicker() string {
	current := styleSelected.Render(m.dp.Time.Format("Mon, Jan 2, 2006"))
	focusLabel := ""
	switch m.dp.Focused {
	case datepicker.FocusHeaderMonth:
		focusLabel = "month"
	case datepicker.FocusHeaderYear:
		focusLabel = "year"
	case datepicker.FocusCalendar:
		focusLabel = "day"
	}
	body := styleTitle.Render("Pick date") + "\n" +
		styleDim.Render("currently:") + " " + current +
		"   " + styleDim.Render("focus:") + " " + styleSelected.Render(focusLabel) + "\n\n" +
		m.dp.View() + "\n\n" +
		styleHelp.Render("←↑→↓ / hjkl: move · tab/shift+tab: month → year → day focus · t: today · enter: select · esc: cancel")
	return stylePanel.Render(body)
}

func (m txModel) viewList() string {
	acctLabel := "All accounts"
	if m.filterAccountID != nil {
		acctLabel = lookupAccount(m.accounts, *m.filterAccountID)
		if acctLabel == "" {
			acctLabel = "(unknown)"
		}
	}
	monthLabel := "All months"
	if m.filterMonth != "" {
		t, err := time.Parse("2006-01", m.filterMonth)
		if err == nil {
			monthLabel = t.Format("Jan 2006")
		} else {
			monthLabel = m.filterMonth
		}
	}

	title := styleTitle.Render(fmt.Sprintf("Transactions (%d)", len(m.rows)))
	chipAcct := styleSelected.Render("▾ " + acctLabel)
	chipMonth := styleSelected.Render("▾ " + monthLabel)
	header := title + "  " + styleDim.Render("account:") + " " + chipAcct +
		"  " + styleDim.Render("month:") + " " + chipMonth

	if len(m.rows) == 0 {
		empty := "No transactions match the current filter."
		if m.filterAccountID == nil && m.filterMonth == "" {
			empty = "No transactions yet. Press n to add one."
		}
		return strings.Join([]string{header, styleDim.Render(empty)}, "\n")
	}
	headers := []string{"Account", "Category / Transfer", "Payee", "Outflow", "Inflow", " "}
	widths := []int{14, 28, 32, 12, 12, 2}

	start, end := m.pageBounds()

	var bodyB strings.Builder
	// Single column-header row, indented to align with the row marker.
	hdrCells := make([]string, len(headers))
	for i, h := range headers {
		hdrCells[i] = styleHeader.Render(padRight(h, widths[i]))
	}
	bodyB.WriteString("  " + lipgloss.JoinHorizontal(lipgloss.Top, hdrCells...) + "\n")

	// Rows are grouped under a date header. A new header is emitted whenever
	// the day changes (and always for the first row of the page), so a page
	// that starts mid-day still shows which day it belongs to.
	lastDay := ""
	for i := start; i < end; i++ {
		t := m.rows[i]
		if day := t.Date.Format("2006-01-02"); day != lastDay {
			bodyB.WriteString(styleTitle.Render(t.Date.Format("Mon, Jan 2, 2006")) + "\n")
			lastDay = day
		}
		// Transfers are read-only on this page (they can't be cleared or
		// edited), so the whole row is dimmed and the cleared column shows a
		// dash instead of a checkbox.
		isTransfer := t.TransferPairID != nil

		acct := truncate(lookupAccount(m.accounts, t.AccountID), 13)
		var catOrTransfer string
		if t.TransferAccountID != nil {
			other := lookupAccount(m.accounts, *t.TransferAccountID)
			arrow := "→ "
			if t.InflowCents > 0 {
				arrow = "← "
			}
			if t.CategoryID != nil {
				catName := truncate(lookupCategory(m.cats, *t.CategoryID), 13)
				catOrTransfer = catName + " " + arrow + truncate(other, 10)
			} else {
				catOrTransfer = arrow + truncate(other, 26)
			}
		} else if t.CategoryID != nil {
			catOrTransfer = truncate(lookupCategory(m.cats, *t.CategoryID), 27)
		} else {
			catOrTransfer = "(uncategorized)"
		}

		outRaw, inRaw := "", ""
		if t.OutflowCents > 0 {
			outRaw = money.Format(t.OutflowCents)
		}
		if t.InflowCents > 0 {
			inRaw = money.Format(t.InflowCents)
		}
		payee := ""
		if t.Payee != nil {
			payee = truncate(*t.Payee, 31)
		}

		var out, in, cleared string
		if isTransfer {
			acct = styleDim.Render(acct)
			catOrTransfer = styleDim.Render(catOrTransfer)
			payee = styleDim.Render(payee)
			out = styleDim.Render(outRaw)
			in = styleDim.Render(inRaw)
			cleared = styleDim.Render("-")
		} else {
			if t.CategoryID == nil {
				catOrTransfer = styleDim.Render(catOrTransfer)
			}
			if outRaw != "" {
				out = styleNeg.Render(outRaw)
			}
			if inRaw != "" {
				in = stylePos.Render(inRaw)
			}
			cleared = " "
			if t.Cleared {
				cleared = stylePos.Render("✓")
			}
		}
		cells := []string{
			padRight(acct, widths[0]),
			padRight(catOrTransfer, widths[1]),
			padRight(payee, widths[2]),
			padRight(out, widths[3]),
			padRight(in, widths[4]),
			padRight(cleared, widths[5]),
		}
		marker := "  "
		if i == m.cursor {
			marker = styleSelected.Render("▸ ")
		}
		line := zone.Mark("tx-row-"+strconv.Itoa(i),
			marker+lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		bodyB.WriteString(line + "\n")
	}
	body := strings.TrimRight(bodyB.String(), "\n")

	pager := ""
	if m.pager.TotalPages > 1 {
		pager = "  " + styleDim.Render(fmt.Sprintf("page %s · rows %d-%d of %d · pgup/pgdn",
			m.pager.View(), start+1, end, len(m.rows)))
	}

	parts := []string{header, body}
	if pager != "" {
		parts = append(parts, pager)
	}
	return strings.Join(parts, "\n")
}
