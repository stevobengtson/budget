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
	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/paydown"
	"github.com/sbengtson/budget/internal/store"
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

	cursor   int // selects which included account is "active"
	horizon  int
	mode     pdMode
	form     form
	picker   picker
	confirm  confirmModel

	pager paginator.Model
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

// debtCents returns the positive owed-balance for a credit/loan account. For
// liability accounts the running balance is negative when in debt, so we
// flip the sign. For other types we use the running balance as-is.
func debtCents(a store.AccountWithBalance) int64 {
	if a.Type.IsLiability() && a.BalanceCents < 0 {
		return -a.BalanceCents
	}
	if a.BalanceCents > 0 {
		return a.BalanceCents
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
			}
		case "down", "j":
			if m.cursor < len(m.included)-1 {
				m.cursor++
			}
		case "a":
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
		case "r", "d":
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
		case "+", "=":
			if m.horizon < pdHorizonMax {
				m.horizon += 12
				return m, m.Refresh()
			}
		case "-", "_":
			if m.horizon > pdHorizonMin {
				m.horizon -= 12
				return m, m.Refresh()
			}
		case "pgdown", "pgdn", "n", ".":
			m.pager.NextPage()
		case "pgup", "p", ",":
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
	banner := fmt.Sprintf("  %s %s  ·  %s %s  ·  %s %s",
		styleDim.Render("Monthly outflow"), stylePos.Render(money.Format(totalMonthly)),
		styleDim.Render("Total interest"), styleNeg.Render(money.Format(totalInterest)),
		styleDim.Render("Longest payoff"), styleWarn.Render(clearLabel),
	)
	b.WriteString(banner)
	b.WriteString("\n\n")

	for i, p := range m.plans {
		acct := m.included[i]
		b.WriteString(zone.Mark("pd-row-"+strconv.Itoa(i), m.renderAccountSection(i, p, acct)))
		b.WriteString("\n")
	}

	if m.pager.TotalPages > 1 {
		fmt.Fprintf(&b, "  %s page %s\n",
			styleDim.Render("◀ , / pgup · . / pgdown ▶"),
			m.pager.View(),
		)
	}
	return b.String()
}

func (m paydownModel) renderAccountSection(idx int, p paydown.Plan, acct store.AccountWithBalance) string {
	var b strings.Builder

	header := fmt.Sprintf("[%s] %.2f%% APR · payment %s/mo · start %s",
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
