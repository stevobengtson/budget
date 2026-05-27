package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/core/format"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
)

type budMode int

const (
	budList budMode = iota
	budAssignForm
	budGoalForm
	budIncomeList
	budIncomeForm
	budIncomeConfirm
)

type budgetModel struct {
	store  *store.Store
	month  string // YYYY-MM
	rows   []store.CategoryBudget
	cursor int
	mode   budMode
	form   form

	incomes       []store.Income
	incomeTotal   int64
	actualIncome  int64
	incomeCursor  int
	editingIncome *store.Income
	incomeConfirm confirmModel

	creditActivity []store.CreditActivity

	width, height int
	scrollOffset  int // top index into m.rows for scrollable category list
}

func newBudgetModel(s *store.Store) budgetModel {
	return budgetModel{store: s, month: store.MonthKey(time.Now())}
}

func (m *budgetModel) SetSize(w, h int) { m.width, m.height = w, h }

// linesAvailable returns how many body lines the category list can use
// (categories AND group headers combined). Subtracts tab bar, title,
// banner, credit section, category column header, scroll indicators,
// status bar, and outer padding.
func (m budgetModel) linesAvailable() int {
	creditLines := 0
	for _, ca := range m.creditActivity {
		if ca.PurchasesCents != 0 || ca.PaymentsCents != 0 {
			creditLines++
		}
	}
	if creditLines > 0 {
		creditLines += 3 // "Credit:" header + cc table header + blank line
	}
	// chrome: tab bar (3) + title (1) + banner (1) + blank (1) + credit
	// (creditLines) + cat header (1) + ↑more (1) + ↓more (1) + blank (1)
	// + status (1) + safety (1)
	chrome := 12 + creditLines
	avail := m.height - chrome
	if avail < 5 {
		avail = 5
	}
	return avail
}

// endForStart returns the exclusive end index of the visible window
// starting at start, accounting for group-header lines emitted between
// category rows.
func (m budgetModel) endForStart(start int) int {
	avail := m.linesAvailable()
	end := start
	used := 0
	lastGroup := ""
	for end < len(m.rows) && used < avail {
		r := m.rows[end]
		cost := 1
		if r.GroupName != lastGroup {
			cost++ // group header line
		}
		if used+cost > avail {
			break
		}
		used += cost
		lastGroup = r.GroupName
		end++
	}
	if end == start && start < len(m.rows) {
		end = start + 1 // always show at least the cursor row
	}
	return end
}

// adjustScroll keeps the cursor visible inside the windowed range.
func (m *budgetModel) adjustScroll() {
	if len(m.rows) == 0 {
		m.scrollOffset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.scrollOffset > m.cursor {
		m.scrollOffset = m.cursor
	}
	for m.endForStart(m.scrollOffset) <= m.cursor && m.scrollOffset < m.cursor {
		m.scrollOffset++
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// formatMonth turns "2006-01" into "Jan 2006". Falls back to input on parse fail.
func formatMonth(m string) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return m
	}
	return t.Format("Jan 2006")
}

func (m budgetModel) modal() bool {
	return m.mode != budList
}

func (m *budgetModel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	switch m.mode {
	case budList:
		for i := range m.rows {
			if zone.Get("bud-row-" + strconv.Itoa(i)).InBounds(msg) {
				m.cursor = i
				return nil
			}
		}
	case budIncomeList:
		for i := range m.incomes {
			if zone.Get("inc-row-" + strconv.Itoa(i)).InBounds(msg) {
				m.incomeCursor = i
				return nil
			}
		}
	}
	return nil
}

func (m budgetModel) Init() tea.Cmd { return m.Refresh() }

func (m *budgetModel) Refresh() tea.Cmd {
	ctx := context.Background()
	rows, err := m.store.MonthBudget(ctx, m.month)
	if err != nil {
		return flashFail("budget: " + err.Error())
	}
	// Hide the system-managed Income category from the list — its data
	// already surfaces in the actual-income banner above.
	filtered := rows[:0]
	for _, r := range rows {
		if !r.IsIncome {
			filtered = append(filtered, r)
		}
	}
	m.rows = filtered
	if m.cursor >= len(m.rows) {
		m.cursor = max0(len(m.rows) - 1)
	}

	incs, err := m.store.ListIncomes(ctx, m.month)
	if err != nil {
		return flashFail("incomes: " + err.Error())
	}
	m.incomes = incs
	if m.incomeCursor >= len(m.incomes) {
		m.incomeCursor = max0(len(m.incomes) - 1)
	}
	total, err := m.store.TotalIncome(ctx, m.month)
	if err != nil {
		return flashFail("income total: " + err.Error())
	}
	m.incomeTotal = total

	actual, err := m.store.ActualIncomeForMonth(ctx, m.month)
	if err != nil {
		return flashFail("actual income: " + err.Error())
	}
	m.actualIncome = actual

	cc, err := m.store.CreditCardActivityForMonth(ctx, m.month)
	if err != nil {
		return flashFail("credit activity: " + err.Error())
	}
	m.creditActivity = cc

	// Clamp scroll after data refresh in case rows shrank.
	m.adjustScroll()
	return nil
}

// signedColored renders a money value with green for positive, red for
// negative, and dim for zero.
func signedColored(cents int64) string {
	s := money.Format(cents)
	switch {
	case cents > 0:
		return stylePos.Render(s)
	case cents < 0:
		return styleNeg.Render(s)
	}
	return styleDim.Render(s)
}

func (m budgetModel) totalAssigned() int64 {
	var t int64
	for _, r := range m.rows {
		t += r.AssignedCents
	}
	return t
}

func (m budgetModel) Update(msg tea.Msg) (budgetModel, tea.Cmd) {
	switch m.mode {
	case budList:
		return m.updateList(msg)
	case budAssignForm:
		return m.updateAssignForm(msg)
	case budGoalForm:
		return m.updateGoalForm(msg)
	case budIncomeList:
		return m.updateIncomeList(msg)
	case budIncomeForm:
		return m.updateIncomeForm(msg)
	case budIncomeConfirm:
		return m.updateIncomeConfirm(msg)
	}
	return m, nil
}

func (m budgetModel) updateList(msg tea.Msg) (budgetModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustScroll()
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.adjustScroll()
			}
		case "pgup", "ctrl+u":
			end := m.endForStart(m.scrollOffset)
			pageSize := end - m.scrollOffset
			if pageSize < 1 {
				pageSize = 1
			}
			m.cursor -= pageSize
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.adjustScroll()
		case "pgdown", "pgdn", "ctrl+d":
			end := m.endForStart(m.scrollOffset)
			pageSize := end - m.scrollOffset
			if pageSize < 1 {
				pageSize = 1
			}
			m.cursor += pageSize
			if m.cursor >= len(m.rows) {
				m.cursor = len(m.rows) - 1
			}
			m.adjustScroll()
		case "home":
			m.cursor = 0
			m.adjustScroll()
		case "end":
			m.cursor = len(m.rows) - 1
			m.adjustScroll()
		case "<", ",", "h":
			m.month = store.PrevMonth(m.month)
			return m, m.Refresh()
		case ">", ".", "l":
			t, _ := time.Parse("2006-01", m.month)
			m.month = t.AddDate(0, 1, 0).Format("2006-01")
			return m, m.Refresh()
		case "t":
			m.month = store.MonthKey(time.Now())
			return m, m.Refresh()
		case "enter":
			if len(m.rows) > 0 {
				m.startAssignForm()
			}
		case "g":
			if len(m.rows) > 0 {
				m.startGoalForm()
			}
		case "i":
			m.mode = budIncomeList
			if m.incomeCursor >= len(m.incomes) {
				m.incomeCursor = max0(len(m.incomes) - 1)
			}
		case "p":
			if len(m.rows) > 0 {
				return m, m.copyAssignedFromPrev()
			}
		}
	}
	return m, nil
}

// copyAssignedFromPrev replaces the highlighted category's assigned
// amount for the active month with whatever was assigned in the previous
// month (0 if the previous month had no row).
func (m *budgetModel) copyAssignedFromPrev() tea.Cmd {
	row := m.rows[m.cursor]
	ctx := context.Background()
	prev := store.PrevMonth(m.month)
	prevCents, err := m.store.GetAssigned(ctx, prev, row.CategoryID)
	if err != nil {
		return flashFail(err.Error())
	}
	if err := m.store.SetAssigned(ctx, m.month, row.CategoryID, prevCents); err != nil {
		return flashFail(err.Error())
	}
	return tea.Batch(m.Refresh(), flashOK("Copied "+money.Format(prevCents)+" from "+formatMonth(prev)))
}

func (m *budgetModel) startAssignForm() {
	r := m.rows[m.cursor]
	cur := money.Format(r.AssignedCents)
	m.form = form{
		fields:   []field{newField("Assigned", cur, "dollars; supports negatives")},
		subtitle: budgetSubtitle(r),
	}
	m.form.SetValues([]string{cur})
	m.form.Focus()
	m.mode = budAssignForm
}

// budgetSubtitle renders the current Assigned / Spent / Available state for
// the category being edited, plus the goal summary when the category has a
// goal set — matching the rendering used in the budget table.
func budgetSubtitle(r store.CategoryBudget) string {
	availStr := money.Format(r.AvailableCents)
	switch {
	case r.AvailableCents < 0:
		availStr = styleNeg.Render(availStr)
	case r.AvailableCents > 0:
		availStr = stylePos.Render(availStr)
	default:
		availStr = styleDim.Render(availStr)
	}
	sep := styleDim.Render("  ·  ")
	out := styleDim.Render("Assigned ") + money.Format(r.AssignedCents) +
		sep + styleDim.Render("Spent ") + money.Format(r.SpentCents) +
		sep + styleDim.Render("Available ") + availStr
	if goal := goalSummary(r); goal != "" {
		out += sep + goal
	}
	return out
}

// goalSummary formats a category's goal as "goal $X by Mon YYYY · need $Y/mo".
// Returns empty string when no goal is set. Shared by the budget table and the
// assign/goal forms so wording stays consistent.
func goalSummary(r store.CategoryBudget) string {
	g, ok := format.GoalFor(r.GoalCents, r.GoalDueDate, r.MonthlyTarget)
	if !ok {
		return ""
	}
	out := "goal " + g.Amount
	if g.Due != "" {
		out += " by " + g.Due
	}
	if g.Need != "" {
		out += styleWarn.Render(" · need " + g.Need)
	}
	return out
}

func (m *budgetModel) startGoalForm() {
	r := m.rows[m.cursor]
	goal := ""
	due := ""
	if r.GoalCents != nil {
		goal = money.Format(*r.GoalCents)
	}
	if r.GoalDueDate != nil {
		due = r.GoalDueDate.Format("2006-01-02")
	}
	m.form = form{
		fields: []field{
			newField("Goal amount", goal, "blank to clear"),
			newField("Due date", due, "YYYY-MM-DD; blank to clear"),
		},
		subtitle: budgetSubtitle(r),
	}
	m.form.SetValues([]string{goal, due})
	m.form.Focus()
	m.mode = budGoalForm
}

func (m budgetModel) updateAssignForm(msg tea.Msg) (budgetModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = budList
		return m, cmd
	}
	if m.form.submitted {
		val := m.form.Values()[0]
		c := int64(0)
		if val != "" {
			parsed, err := money.Parse(val)
			if err != nil {
				m.form.err = err.Error()
				return m, cmd
			}
			c = parsed
		}
		row := m.rows[m.cursor]
		if err := m.store.SetAssigned(context.Background(), m.month, row.CategoryID, c); err != nil {
			m.form.err = err.Error()
			return m, cmd
		}
		m.mode = budList
		return m, tea.Batch(m.Refresh(), flashOK("Assigned"))
	}
	return m, cmd
}

func (m budgetModel) updateGoalForm(msg tea.Msg) (budgetModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = budList
		return m, cmd
	}
	if m.form.submitted {
		vals := m.form.Values()
		var goalPtr *int64
		var duePtr *time.Time
		if vals[0] != "" {
			c, err := money.Parse(vals[0])
			if err != nil {
				m.form.err = "amount: " + err.Error()
				return m, cmd
			}
			goalPtr = &c
		}
		if vals[1] != "" {
			t, err := time.Parse("2006-01-02", vals[1])
			if err != nil {
				m.form.err = "date: " + err.Error()
				return m, cmd
			}
			duePtr = &t
		}
		row := m.rows[m.cursor]
		// Re-load full category so we don't drop sort_order/etc.
		cats, err := m.store.ListCategories(context.Background(), true)
		if err != nil {
			m.form.err = err.Error()
			return m, cmd
		}
		for _, c := range cats {
			if c.ID == row.CategoryID {
				c.GoalCents = goalPtr
				c.GoalDueDate = duePtr
				if err := m.store.UpdateCategory(context.Background(), c); err != nil {
					m.form.err = err.Error()
					return m, cmd
				}
				break
			}
		}
		m.mode = budList
		return m, tea.Batch(m.Refresh(), flashOK("Goal updated"))
	}
	return m, cmd
}

func (m budgetModel) View() string {
	switch m.mode {
	case budAssignForm:
		return m.form.View("Assign — " + m.rows[m.cursor].CategoryName + " — " + formatMonth(m.month))
	case budGoalForm:
		return m.form.View("Goal — " + m.rows[m.cursor].CategoryName)
	case budIncomeList:
		return m.viewIncomeList()
	case budIncomeForm:
		title := "New income · " + formatMonth(m.month)
		if m.editingIncome != nil {
			title = "Edit income · " + formatMonth(m.month)
		}
		return m.form.View(title)
	case budIncomeConfirm:
		return m.incomeConfirm.View()
	}
	return m.viewList()
}

// --- Income panel ---

func (m budgetModel) updateIncomeList(msg tea.Msg) (budgetModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.mode = budList
			return m, nil
		case "up", "k":
			if m.incomeCursor > 0 {
				m.incomeCursor--
			}
		case "down", "j":
			if m.incomeCursor < len(m.incomes)-1 {
				m.incomeCursor++
			}
		case "n":
			m.startIncomeForm(nil)
		case "enter":
			if len(m.incomes) > 0 {
				inc := m.incomes[m.incomeCursor]
				m.startIncomeForm(&inc)
			}
		case "d":
			if len(m.incomes) > 0 {
				m.incomeConfirm = confirmModel{prompt: fmt.Sprintf("Delete income %q?", m.incomes[m.incomeCursor].Name)}
				m.mode = budIncomeConfirm
			}
		case "p":
			return m, m.copyIncomeFromPrev()
		}
	}
	return m, nil
}

// copyIncomeFromPrev mirrors the web "copy from previous month" action:
// for every entry in the previous month, either insert it (new name) or
// update the current-month row with the same name to the previous amount.
// Entries that only exist in the current month are left alone. The
// (month, name) unique constraint means a blind re-insert would error;
// the upsert side-steps that.
func (m *budgetModel) copyIncomeFromPrev() tea.Cmd {
	ctx := context.Background()
	prev := store.PrevMonth(m.month)
	prevRows, err := m.store.ListIncomes(ctx, prev)
	if err != nil {
		return flashFail(err.Error())
	}
	if len(prevRows) == 0 {
		return flashFail("No income in " + formatMonth(prev))
	}
	existing := make(map[string]store.Income, len(m.incomes))
	for _, r := range m.incomes {
		existing[r.Name] = r
	}
	for _, r := range prevRows {
		if cur, ok := existing[r.Name]; ok {
			cur.AmountCents = r.AmountCents
			cur.SortOrder = r.SortOrder
			if err := m.store.UpdateIncome(ctx, cur); err != nil {
				return flashFail(err.Error())
			}
			continue
		}
		if _, err := m.store.CreateIncome(ctx, store.Income{
			Month:       m.month,
			Name:        r.Name,
			AmountCents: r.AmountCents,
			SortOrder:   r.SortOrder,
		}); err != nil {
			return flashFail(err.Error())
		}
	}
	return tea.Batch(m.Refresh(), flashOK(fmt.Sprintf("Copied %d entries from %s", len(prevRows), formatMonth(prev))))
}

func (m *budgetModel) startIncomeForm(existing *store.Income) {
	m.editingIncome = existing
	fields := []field{
		newField("Name", "Work", ""),
		newField("Amount", "0.00", "estimated $ for "+formatMonth(m.month)),
	}
	m.form = form{fields: fields}
	if existing != nil {
		m.form.SetValues([]string{existing.Name, money.Format(existing.AmountCents)})
	}
	m.form.Focus()
	m.mode = budIncomeForm
}

func (m budgetModel) updateIncomeForm(msg tea.Msg) (budgetModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = budIncomeList
		return m, cmd
	}
	if m.form.submitted {
		vals := m.form.Values()
		if vals[0] == "" {
			m.form.err = "name required"
			return m, cmd
		}
		amt := int64(0)
		if vals[1] != "" {
			c, err := money.Parse(vals[1])
			if err != nil {
				m.form.err = "amount: " + err.Error()
				return m, cmd
			}
			amt = c
		}
		ctx := context.Background()
		if m.editingIncome != nil {
			if err := m.store.UpdateIncome(ctx, store.Income{
				ID: m.editingIncome.ID, Name: vals[0], AmountCents: amt,
				SortOrder: m.editingIncome.SortOrder,
			}); err != nil {
				m.form.err = err.Error()
				return m, cmd
			}
		} else {
			if _, err := m.store.CreateIncome(ctx, store.Income{
				Month: m.month, Name: vals[0], AmountCents: amt,
				SortOrder: int64(len(m.incomes)),
			}); err != nil {
				m.form.err = err.Error()
				return m, cmd
			}
		}
		m.mode = budIncomeList
		return m, tea.Batch(m.Refresh(), flashOK("Income saved"))
	}
	return m, cmd
}

func (m budgetModel) updateIncomeConfirm(msg tea.Msg) (budgetModel, tea.Cmd) {
	var cmd tea.Cmd
	m.incomeConfirm, cmd = m.incomeConfirm.Update(msg)
	if m.incomeConfirm.answered {
		if m.incomeConfirm.yes && len(m.incomes) > 0 {
			id := m.incomes[m.incomeCursor].ID
			if err := m.store.DeleteIncome(context.Background(), id); err != nil {
				cmd = tea.Batch(cmd, flashFail(err.Error()))
			} else {
				cmd = tea.Batch(cmd, flashOK("Deleted"))
			}
			cmd = tea.Batch(cmd, m.Refresh())
		}
		m.mode = budIncomeList
	}
	return m, cmd
}

func (m budgetModel) viewIncomeList() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Income · " + formatMonth(m.month)))
	b.WriteString("\n\n")

	if len(m.incomes) == 0 {
		b.WriteString(styleDim.Render("No income entries for this month. Press n to add one."))
		b.WriteString("\n")
	} else {
		headers := []string{"Source", "Amount"}
		widths := []int{28, 16}
		hdrCells := make([]string, len(headers))
		for i, h := range headers {
			hdrCells[i] = styleHeader.Render(padRight(h, widths[i]))
		}
		b.WriteString("  " + strings.Join(hdrCells, ""))
		b.WriteString("\n")
		for i, inc := range m.incomes {
			marker := "  "
			if i == m.incomeCursor {
				marker = styleSelected.Render("▸ ")
			}
			amt := stylePos.Render(money.Format(inc.AmountCents))
			line := marker + padRight(inc.Name, widths[0]) + padRight(amt, widths[1])
			b.WriteString(zone.Mark("inc-row-"+strconv.Itoa(i), line) + "\n")
		}
	}

	assigned := m.totalAssigned()
	remain := m.incomeTotal - assigned
	b.WriteString("\n")
	b.WriteString(padRight("  Total income", 28) + stylePos.Render(money.Format(m.incomeTotal)) + "\n")
	b.WriteString(padRight("  Budgeted", 28) + money.Format(assigned) + "\n")
	remStyled := stylePos.Render(money.Format(remain))
	if remain < 0 {
		remStyled = styleNeg.Render(money.Format(remain))
	}
	b.WriteString(padRight("  Remain", 28) + remStyled + "\n")

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("j/k: move · n: new · enter: edit · d: delete · p: copy prev month · esc: back to budget"))
	return b.String()
}

func (m budgetModel) viewList() string {
	if len(m.rows) == 0 {
		return styleDim.Render("No categories. Add some on the Categories tab.")
	}

	headers := []string{"Group / Category", "Assigned", "Spent", "Available", "Goal"}
	widths := []int{30, 12, 12, 14, 28}

	// Build a custom render so cursor lines up with category rows only.
	var b strings.Builder
	b.WriteString(styleTitle.Render("Budget · " + formatMonth(m.month)))
	b.WriteString("\n")

	// Income / budgeted / remain banner. "Estimated" is the user-entered
	// forecast (Income panel via `i`); "Actual" is the sum of inflows
	// categorized as Income for the month.
	assigned := m.totalAssigned()
	remain := m.incomeTotal - assigned // Estimated − Budgeted
	gap := m.incomeTotal - m.actualIncome
	banner := fmt.Sprintf("  %s %s  ·  %s %s  ·  %s %s  ·  %s %s  ·  %s %s",
		styleDim.Render("Estimated"), stylePos.Render(money.Format(m.incomeTotal)),
		styleDim.Render("Actual"), stylePos.Render(money.Format(m.actualIncome)),
		styleDim.Render("Budgeted"), money.Format(assigned),
		styleDim.Render("Remain"), signedColored(remain),
		styleDim.Render("Est−Act"), signedColored(gap),
	)
	b.WriteString(banner)
	b.WriteString("\n")

	// Credit section: this-month purchases minus payments per credit
	// account. Skip cards with zero activity to keep the section tidy.
	hasCreditActivity := false
	for _, ca := range m.creditActivity {
		if ca.PurchasesCents > 0 || ca.PaymentsCents > 0 {
			hasCreditActivity = true
			break
		}
	}
	if hasCreditActivity {
		b.WriteString("\n")
		b.WriteString(styleHeader.Render("  Credit:"))
		b.WriteString("\n")

		ccHeaders := []string{"Account", "Purchases", "Payments", "Owing"}
		ccWidths := []int{20, 14, 14, 14}
		ccRows := make([][]string, 0, len(m.creditActivity))
		for _, ca := range m.creditActivity {
			if ca.PurchasesCents == 0 && ca.PaymentsCents == 0 {
				continue
			}
			ccRows = append(ccRows, []string{
				ca.AccountName,
				styleNeg.Render(money.Format(ca.PurchasesCents)),
				stylePos.Render(money.Format(ca.PaymentsCents)),
				signedColored(ca.OwingCents),
			})
		}
		// renderTable prefixes each row with "  " when no row is selected,
		// indenting it cleanly under the "Credit:" header.
		b.WriteString(renderTable(ccHeaders, ccWidths, ccRows, -1, ""))
	}
	b.WriteString("\n")

	hdrCells := make([]string, len(headers))
	for i, h := range headers {
		hdrCells[i] = styleHeader.Render(padRight(h, widths[i]))
	}
	b.WriteString("  " + strings.Join(hdrCells, ""))
	b.WriteString("\n")

	// Compute scroll window using line-budget. Group headers consume one
	// line each — the window sizer accounts for them.
	start := m.scrollOffset
	if start < 0 {
		start = 0
	}
	if start >= len(m.rows) {
		start = max0(len(m.rows) - 1)
	}
	end := m.endForStart(start)

	if start > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("  ↑ %d more above\n", start)))
	}

	lastGroup := ""
	for i := start; i < end; i++ {
		r := m.rows[i]

		// Emit group header when the group changes (and always above the
		// first row of the window).
		if r.GroupName != lastGroup {
			groupLine := padRight(styleHeader.Render("["+r.GroupName+"]"), widths[0]) +
				strings.Repeat(" ", widths[1]+widths[2]+widths[3]+widths[4])
			b.WriteString("  " + groupLine + "\n")
			lastGroup = r.GroupName
		}

		availStr := money.Format(r.AvailableCents)
		switch {
		case r.AvailableCents < 0:
			availStr = styleNeg.Render(availStr)
		case r.AvailableCents > 0:
			availStr = stylePos.Render(availStr)
		default:
			availStr = styleDim.Render(availStr)
		}
		goalCol := goalSummary(r)
		cells := []string{
			padRight("  "+r.CategoryName, widths[0]),
			padRight(money.Format(r.AssignedCents), widths[1]),
			padRight(money.Format(r.SpentCents), widths[2]),
			padRight(availStr, widths[3]),
			padRight(goalCol, widths[4]),
		}
		line := strings.Join(cells, "")
		marker := "  "
		if i == m.cursor {
			marker = styleSelected.Render("▸ ")
		}
		b.WriteString(zone.Mark("bud-row-"+strconv.Itoa(i), marker+line))
		b.WriteString("\n")
	}

	if end < len(m.rows) {
		b.WriteString(styleDim.Render(fmt.Sprintf("  ↓ %d more below\n", len(m.rows)-end)))
	}

	b.WriteString("\n")
	return b.String()
}
