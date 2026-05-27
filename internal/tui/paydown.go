package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/paydown"
	"github.com/sbengtson/budget/internal/core/store"
)

type pdMode int

const (
	pdList pdMode = iota
	pdPaymentForm
	pdAddPick
	pdRemoveConfirm
	pdCategoryPick
)

const (
	pdHorizonDefault = 60
	pdHorizonMin     = 12
	pdHorizonMax     = 360
	pdPageSize       = 12
)

type paydownModel struct {
	store *store.Store

	// All non-archived accounts, used for the add-picker. paydownEligible
	// is the subset that participates in the projection.
	all      []store.AccountWithBalance
	included []store.AccountWithBalance
	plans    []paydown.Plan

	cursor  int // selects which included account is "active"
	horizon int
	mode    pdMode
	form    form
	picker  picker
	confirm confirmModel

	pager paginator.Model

	width, height       int
	sectionScrollOffset int // index of first visible account section
}

func (m *paydownModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.adjustSectionScroll()
}

// linesPerSection returns the number of lines a single account section
// will emit (header + table header + page rows + ellipsis + blank).
func (m paydownModel) linesPerSection(p paydown.Plan) int {
	if len(p.Rows) == 0 {
		return 3 // header + "(already paid off)" + blank
	}
	page := pdPageSize
	rowsOnPage := len(p.Rows) - m.pager.Page*pdPageSize
	if rowsOnPage > page {
		rowsOnPage = page
	}
	if rowsOnPage < 0 {
		rowsOnPage = 0
	}
	// header + table header + rowsOnPage + ellipsis (1 if remaining) + blank
	lines := 2 + rowsOnPage + 1
	return lines
}

// adjustSectionScroll keeps the cursor's section in the visible area.
func (m *paydownModel) adjustSectionScroll() {
	if len(m.plans) == 0 {
		m.sectionScrollOffset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.plans) {
		m.cursor = len(m.plans) - 1
	}
	if m.sectionScrollOffset > m.cursor {
		m.sectionScrollOffset = m.cursor
	}
	avail := m.linesAvailable()
	for {
		used := 0
		end := m.sectionScrollOffset
		for end < len(m.plans) && used < avail {
			cost := m.linesPerSection(m.plans[end])
			if used+cost > avail && end > m.sectionScrollOffset {
				break
			}
			used += cost
			end++
		}
		if m.cursor < end {
			break
		}
		m.sectionScrollOffset++
		if m.sectionScrollOffset >= len(m.plans) {
			m.sectionScrollOffset = len(m.plans) - 1
			break
		}
	}
}

// linesAvailable for paydown sections.
func (m paydownModel) linesAvailable() int {
	// chrome: tab bar (3) + title (1) + banner (1) + blank (1) + ↑more (1)
	// + ↓more (1) + pager line (1) + status (1) + safety (2)
	chrome := 12
	avail := m.height - chrome
	if avail < 6 {
		avail = 6
	}
	return avail
}

func newPaydownModel(s *store.Store) paydownModel {
	pg := paginator.New()
	pg.Type = paginator.Arabic
	pg.PerPage = pdPageSize
	return paydownModel{store: s, horizon: pdHorizonDefault, pager: pg}
}

// longestPlan returns the row count of the largest plan, used to size pages.
func (m paydownModel) longestPlan() int {
	n := 0
	for _, p := range m.plans {
		if len(p.Rows) > n {
			n = len(p.Rows)
		}
	}
	return n
}

func (m paydownModel) modal() bool { return m.mode != pdList }

func (m *paydownModel) Refresh() tea.Cmd {
	ctx := context.Background()
	all, err := m.store.ListAccounts(ctx, false)
	if err != nil {
		return flashFail("paydown accounts: " + err.Error())
	}
	m.all = all

	included := make([]store.AccountWithBalance, 0)
	for _, a := range all {
		if a.IncludeInPaydown && a.AprBps != nil {
			included = append(included, a)
		}
	}
	m.included = included

	plans := make([]paydown.Plan, 0, len(included))
	now := time.Now()
	for _, a := range included {
		startCents := debtCents(a)
		var fallback int64
		if a.MonthlyPaymentCents != nil {
			fallback = *a.MonthlyPaymentCents
		}
		storeSched, err := m.store.PaymentScheduleForCategory(ctx, a.PaymentCategoryID, now, m.horizon, fallback)
		if err != nil {
			return flashFail("payment schedule: " + err.Error())
		}
		schedule := make([]paydown.MonthPayment, len(storeSched))
		for i, sm := range storeSched {
			schedule[i] = paydown.MonthPayment{
				Cents:  sm.Cents,
				Source: convertPaymentSource(sm.Source),
			}
		}
		p, err := paydown.Compute(a.ID, a.Name, *a.AprBps, startCents, schedule, now)
		if err != nil {
			return flashFail("paydown compute: " + err.Error())
		}
		plans = append(plans, p)
	}
	m.plans = plans
	if m.cursor >= len(m.included) {
		m.cursor = max0(len(m.included) - 1)
	}
	// Resize paginator to span the longest projection.
	m.pager.SetTotalPages(m.longestPlan())
	if m.pager.Page >= m.pager.TotalPages {
		m.pager.Page = max0(m.pager.TotalPages - 1)
	}
	m.adjustSectionScroll()
	return nil
}

func convertPaymentSource(s store.PaymentSource) paydown.PaymentSource {
	switch s {
	case store.PaymentSpent:
		return paydown.SourceSpent
	case store.PaymentAssigned:
		return paydown.SourceAssigned
	}
	return paydown.SourceDefault
}

// debtCents returns the positive owed-balance for any account whose running
// balance has gone negative — credit / loan in debt, or a checking / savings
// account in overdraft. Positive-balance accounts have no debt.
func debtCents(a store.AccountWithBalance) int64 {
	if a.BalanceCents < 0 {
		return -a.BalanceCents
	}
	return 0
}

func (m *paydownModel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != pdList {
		return nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	for i := range m.included {
		if zone.Get("pd-row-" + strconv.Itoa(i)).InBounds(msg) {
			m.cursor = i
			return nil
		}
	}
	return nil
}

func (m paydownModel) Update(msg tea.Msg) (paydownModel, tea.Cmd) {
	switch m.mode {
	case pdList:
		return m.updateList(msg)
	case pdPaymentForm:
		return m.updatePaymentForm(msg)
	case pdAddPick:
		return m.updateAddPicker(msg)
	case pdCategoryPick:
		return m.updateCategoryPicker(msg)
	case pdRemoveConfirm:
		return m.updateRemoveConfirm(msg)
	}
	return m, nil
}

func (m paydownModel) updateCategoryPicker(msg tea.Msg) (paydownModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = pdList
		return m, cmd
	}
	if m.picker.chosen {
		ctx := context.Background()
		acct, err := m.store.GetAccount(ctx, m.included[m.cursor].ID)
		if err != nil {
			m.mode = pdList
			return m, flashFail(err.Error())
		}
		if m.picker.cursor == 0 {
			acct.PaymentCategoryID = nil
		} else {
			catName := stripGroupPrefix(m.picker.items[m.picker.cursor])
			cats, _ := m.store.ListCategories(ctx, false)
			for _, c := range cats {
				if c.Name == catName {
					id := c.ID
					acct.PaymentCategoryID = &id
					break
				}
			}
		}
		if err := m.store.UpdateAccount(ctx, acct); err != nil {
			m.mode = pdList
			return m, flashFail(err.Error())
		}
		m.mode = pdList
		return m, tea.Batch(m.Refresh(), flashOK("Category linked"))
	}
	return m, cmd
}

func (m paydownModel) updateList(msg tea.Msg) (paydownModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.adjustSectionScroll()
			}
		case "down", "j":
			if m.cursor < len(m.included)-1 {
				m.cursor++
				m.adjustSectionScroll()
			}
		case "n":
			candidates := m.eligibleToAdd()
			if len(candidates) == 0 {
				return m, flashFail("no eligible accounts (need APR set + not already included)")
			}
			items := make([]string, len(candidates))
			for i, a := range candidates {
				items[i] = fmt.Sprintf("%s · %.2f%% APR · %s owed",
					a.Name, float64(*a.AprBps)/100.0, money.Format(debtCents(a)))
			}
			m.picker = newPicker("Add account to paydown", items, 0)
			m.mode = pdAddPick
		case "e":
			if len(m.included) > 0 {
				m.startPaymentForm()
			}
		case "d":
			if len(m.included) > 0 {
				m.confirm = confirmModel{prompt: "Remove " + m.included[m.cursor].Name + " from paydown?"}
				m.mode = pdRemoveConfirm
			}
		case "c":
			if len(m.included) > 0 {
				cats, err := m.store.ListCategories(context.Background(), false)
				if err != nil {
					return m, flashFail(err.Error())
				}
				groups, _ := m.store.ListGroups(context.Background())
				items := []string{"(none — fall back to default)"}
				cur := 0
				curID := m.included[m.cursor].PaymentCategoryID
				for _, g := range groups {
					for _, c := range cats {
						if c.GroupID == g.ID {
							items = append(items, g.Name+" · "+c.Name)
							if curID != nil && c.ID == *curID {
								cur = len(items) - 1
							}
						}
					}
				}
				m.picker = newPicker("Link payment category", items, cur)
				m.mode = pdCategoryPick
			}
		// Horizon controls. Paydown has no calendar month, so the same
		// h / l keys that mean "prev/next month" elsewhere shrink and
		// extend the projection horizon here.
		case "h":
			if m.horizon > pdHorizonMin {
				m.horizon -= 12
				return m, m.Refresh()
			}
		case "l":
			if m.horizon < pdHorizonMax {
				m.horizon += 12
				return m, m.Refresh()
			}
		case "pgdown", "pgdn", "ctrl+d":
			m.pager.NextPage()
		case "pgup", "ctrl+u":
			m.pager.PrevPage()
		case "home":
			m.pager.Page = 0
		case "end":
			m.pager.Page = max0(m.pager.TotalPages - 1)
		}
	}
	return m, nil
}

func (m *paydownModel) eligibleToAdd() []store.AccountWithBalance {
	out := make([]store.AccountWithBalance, 0)
	for _, a := range m.all {
		if a.IncludeInPaydown {
			continue
		}
		if a.AprBps == nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (m *paydownModel) startPaymentForm() {
	a := m.included[m.cursor]
	pay := ""
	if a.MonthlyPaymentCents != nil {
		pay = money.Format(*a.MonthlyPaymentCents)
	}
	m.form = form{fields: []field{
		newField("Monthly payment", pay, "for "+a.Name),
	}}
	m.form.SetValues([]string{pay})
	m.form.Focus()
	m.mode = pdPaymentForm
}

func (m paydownModel) updatePaymentForm(msg tea.Msg) (paydownModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = pdList
		return m, cmd
	}
	if m.form.submitted {
		val := m.form.Values()[0]
		var payPtr *int64
		if val != "" {
			c, err := money.Parse(val)
			if err != nil {
				m.form.err = err.Error()
				return m, cmd
			}
			payPtr = &c
		}
		acct, err := m.store.GetAccount(context.Background(), m.included[m.cursor].ID)
		if err != nil {
			m.form.err = err.Error()
			return m, cmd
		}
		acct.MonthlyPaymentCents = payPtr
		if err := m.store.UpdateAccount(context.Background(), acct); err != nil {
			m.form.err = err.Error()
			return m, cmd
		}
		m.mode = pdList
		return m, tea.Batch(m.Refresh(), flashOK("Payment saved"))
	}
	return m, cmd
}

func (m paydownModel) updateAddPicker(msg tea.Msg) (paydownModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = pdList
		return m, cmd
	}
	if m.picker.chosen {
		candidates := m.eligibleToAdd()
		if m.picker.cursor < len(candidates) {
			a, err := m.store.GetAccount(context.Background(), candidates[m.picker.cursor].ID)
			if err != nil {
				m.mode = pdList
				return m, flashFail(err.Error())
			}
			a.IncludeInPaydown = true
			if err := m.store.UpdateAccount(context.Background(), a); err != nil {
				m.mode = pdList
				return m, flashFail(err.Error())
			}
		}
		m.mode = pdList
		return m, tea.Batch(m.Refresh(), flashOK("Added to paydown"))
	}
	return m, cmd
}

func (m paydownModel) updateRemoveConfirm(msg tea.Msg) (paydownModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	if m.confirm.answered {
		if m.confirm.yes && len(m.included) > 0 {
			a, err := m.store.GetAccount(context.Background(), m.included[m.cursor].ID)
			if err != nil {
				cmd = tea.Batch(cmd, flashFail(err.Error()))
			} else {
				a.IncludeInPaydown = false
				if err := m.store.UpdateAccount(context.Background(), a); err != nil {
					cmd = tea.Batch(cmd, flashFail(err.Error()))
				} else {
					cmd = tea.Batch(cmd, m.Refresh(), flashOK("Removed"))
				}
			}
		}
		m.mode = pdList
	}
	return m, cmd
}

func (m paydownModel) View() string {
	switch m.mode {
	case pdPaymentForm:
		return m.form.View("Edit monthly payment")
	case pdAddPick, pdCategoryPick:
		return m.picker.View()
	case pdRemoveConfirm:
		return m.confirm.View()
	}
	return m.viewList()
}

func (m paydownModel) viewList() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Debt Paydown · " + strconv.Itoa(m.horizon) + " month horizon"))
	b.WriteString("\n")

	if len(m.included) == 0 {
		b.WriteString("\n")
		b.WriteString(styleDim.Render("No accounts in the paydown plan yet."))
		b.WriteString("\n\n")
		return b.String()
	}

	// Aggregate totals.
	var totalMonthly, totalInterest int64
	longest := 0
	allCleared := true
	for _, p := range m.plans {
		totalMonthly += p.PaymentCents
		totalInterest += p.TotalInterestCents
		if p.PayoffMonth.IsZero() {
			allCleared = false
		}
		if len(p.Rows) > longest {
			longest = len(p.Rows)
		}
	}
	clearLabel := strconv.Itoa(longest) + " months"
	if !allCleared {
		clearLabel = "won't clear in horizon"
	}
	banner := fmt.Sprintf(
		"  %s %s  ·  %s %s  ·  %s %s",
		styleDim.Render("Monthly outflow"), stylePos.Render(money.Format(totalMonthly)),
		styleDim.Render("Total interest"), styleNeg.Render(money.Format(totalInterest)),
		styleDim.Render("Longest payoff"), styleWarn.Render(clearLabel),
	)
	b.WriteString(banner)
	b.WriteString("\n\n")

	// Determine the visible range of sections based on line budget.
	avail := m.linesAvailable()
	start := m.sectionScrollOffset
	if start < 0 {
		start = 0
	}
	if start >= len(m.plans) {
		start = max0(len(m.plans) - 1)
	}
	end := start
	used := 0
	for end < len(m.plans) {
		cost := m.linesPerSection(m.plans[end])
		if used+cost > avail && end > start {
			break
		}
		used += cost
		end++
	}

	if start > 0 {
		fmt.Fprintf(&b, "  %s\n", styleDim.Render(fmt.Sprintf("↑ %d more above", start)))
	}

	for i := start; i < end; i++ {
		p := m.plans[i]
		acct := m.included[i]
		b.WriteString(zone.Mark("pd-row-"+strconv.Itoa(i), m.renderAccountSection(i, p, acct)))
		b.WriteString("\n")
	}

	if end < len(m.plans) {
		fmt.Fprintf(&b, "  %s\n", styleDim.Render(fmt.Sprintf("↓ %d more below", len(m.plans)-end)))
	}

	if m.pager.TotalPages > 1 {
		fmt.Fprintf(
			&b, "  page %s\n",
			m.pager.View(),
		)
	}
	return b.String()
}

func (m paydownModel) renderAccountSection(idx int, p paydown.Plan, acct store.AccountWithBalance) string {
	var b strings.Builder

	header := fmt.Sprintf(
		"[%s] %.2f%% APR · payment %s/mo · start %s",
		acct.Name,
		float64(p.AprBps)/100.0,
		money.Format(p.PaymentCents),
		money.Format(p.StartCents),
	)
	if acct.PaymentCategoryID != nil {
		// Look up the category name via store list (cached on parent screen
		// would be ideal — fetch lazily here for simplicity).
		if cats, err := m.store.ListCategories(context.Background(), true); err == nil {
			for _, c := range cats {
				if c.ID == *acct.PaymentCategoryID {
					header += " · category " + c.Name
					break
				}
			}
		}
	} else {
		header += " · " + styleWarn.Render("no category linked — press c to link")
	}
	if !p.PayoffMonth.IsZero() {
		header += " · clears " + p.PayoffMonth.Format("Jan 2006")
		header += " · interest " + money.Format(p.TotalInterestCents)
	} else if p.Diverging {
		header += " · " + styleErr.Render("payment ≤ interest, debt grows")
	} else {
		header += " · " + styleWarn.Render("not paid off in horizon")
	}

	if idx == m.cursor {
		b.WriteString(styleSelected.Render("▸ " + header))
	} else {
		b.WriteString("  " + styleHeader.Render(header))
	}
	b.WriteString("\n")

	if len(p.Rows) == 0 {
		b.WriteString(styleDim.Render("  (already paid off)"))
		b.WriteString("\n")
		return b.String()
	}

	headers := []string{"Month", "Interest", "Payment", "Source", "Balance"}
	widths := []int{12, 12, 12, 12, 14}
	hdrCells := make([]string, len(headers))
	for i, h := range headers {
		hdrCells[i] = styleHeader.Render(padRight(h, widths[i]))
	}
	b.WriteString("    " + strings.Join(hdrCells, ""))
	b.WriteString("\n")

	start := m.pager.Page * pdPageSize
	if start >= len(p.Rows) {
		b.WriteString(styleDim.Render("    (no rows on this page — paid off earlier)"))
		b.WriteString("\n")
		return b.String()
	}
	end := start + pdPageSize
	if end > len(p.Rows) {
		end = len(p.Rows)
	}
	for i := start; i < end; i++ {
		r := p.Rows[i]
		var src string
		switch r.PaymentSource {
		case paydown.SourceSpent:
			src = stylePos.Render("✓ spent")
		case paydown.SourceAssigned:
			src = styleWarn.Render("→ assigned")
		default:
			src = styleDim.Render("· default")
		}
		cells := []string{
			padRight(r.Month.Format("Jan 2006"), widths[0]),
			padRight(styleNeg.Render(money.Format(r.InterestCents)), widths[1]),
			padRight(stylePos.Render(money.Format(r.PaymentCents)), widths[2]),
			padRight(src, widths[3]),
			padRight(money.Format(r.BalanceCents), widths[4]),
		}
		b.WriteString("    " + strings.Join(cells, ""))
		b.WriteString("\n")
	}
	if remaining := len(p.Rows) - end; remaining > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("    showing %d-%d of %d months", start+1, end, len(p.Rows))))
		b.WriteString("\n")
	} else if start > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("    showing %d-%d of %d months (last page)", start+1, end, len(p.Rows))))
		b.WriteString("\n")
	}
	return b.String()
}
