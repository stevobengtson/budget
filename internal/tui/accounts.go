package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/core/money"
	"github.com/sbengtson/budget/internal/core/store"
)

type acctMode int

const (
	acctList acctMode = iota
	acctForm
	acctTypePick
	acctCategoryPick
	acctConfirmDel
)

type accountsModel struct {
	store   *store.Store
	rows    []store.AccountWithBalance
	cursor  int
	mode    acctMode
	form    form
	picker  picker
	confirm confirmModel
	editing *store.Account // nil = new

	// cached for payment-category picker
	cats   []store.Category
	groups []store.CategoryGroup
}

func newAccountsModel(s *store.Store) accountsModel { return accountsModel{store: s} }

func (m accountsModel) modal() bool { return m.mode != acctList }

// HandleMouse processes left-click row selection.
func (m *accountsModel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != acctList {
		return nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	for i := range m.rows {
		if zone.Get("acct-row-" + strconv.Itoa(i)).InBounds(msg) {
			m.cursor = i
			return nil
		}
	}
	return nil
}

func (m accountsModel) Init() tea.Cmd { return m.Refresh() }

func (m *accountsModel) Refresh() tea.Cmd {
	ctx := context.Background()
	cats, _ := m.store.ListCategories(ctx, false)
	groups, _ := m.store.ListGroups(ctx)
	m.cats = cats
	m.groups = groups
	rows, err := m.store.ListAccounts(ctx, false)
	if err != nil {
		return flashFail("load accounts: " + err.Error())
	}
	m.rows = rows
	if m.cursor >= len(m.rows) {
		m.cursor = max0(len(m.rows) - 1)
	}
	return nil
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func (m accountsModel) Update(msg tea.Msg) (accountsModel, tea.Cmd) {
	switch m.mode {
	case acctList:
		return m.updateList(msg)
	case acctForm:
		return m.updateForm(msg)
	case acctTypePick:
		return m.updateTypePicker(msg)
	case acctCategoryPick:
		return m.updateCategoryPicker(msg)
	case acctConfirmDel:
		return m.updateConfirm(msg)
	}
	return m, nil
}

func (m accountsModel) updateList(msg tea.Msg) (accountsModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "n":
			m.startForm(nil)
		case "enter":
			if len(m.rows) > 0 {
				a := m.rows[m.cursor].Account
				m.startForm(&a)
			}
		case "d":
			if len(m.rows) > 0 {
				m.confirm = confirmModel{prompt: fmt.Sprintf("Archive account %q? Existing transactions are kept.", m.rows[m.cursor].Name)}
				m.mode = acctConfirmDel
			}
		}
	}
	return m, nil
}

func (m *accountsModel) startForm(existing *store.Account) {
	m.editing = existing
	fields := []field{
		newField("Name", "Checking", ""),
		newField("Type", "checking|savings|cash|credit|loan", "press space to pick"),
		newField("Starting balance", "0.00", "dollars · for credit/loan enter amount owed (auto-stored negative)"),
		newField("Credit / overdraft limit", "", "blank if N/A · also used for checking overdraft"),
		newField("APR %", "", "annual % · only accrues while balance is negative"),
		newField("Monthly payment (paydown)", "", "fallback when no budget data"),
		newField("Include in paydown", "no", "yes/no"),
		newField("Payment category (paydown)", "", "space to pick — pulls from budget"),
	}
	m.form = form{fields: fields}
	if existing != nil {
		var lim, apr, pay string
		if existing.CreditLimitCents != nil {
			lim = money.Format(*existing.CreditLimitCents)
		}
		if existing.AprBps != nil {
			apr = fmt.Sprintf("%.2f", float64(*existing.AprBps)/100.0)
		}
		if existing.MonthlyPaymentCents != nil {
			pay = money.Format(*existing.MonthlyPaymentCents)
		}
		include := "no"
		if existing.IncludeInPaydown {
			include = "yes"
		}
		catName := ""
		if existing.PaymentCategoryID != nil {
			for _, c := range m.cats {
				if c.ID == *existing.PaymentCategoryID {
					catName = c.Name
					break
				}
			}
		}
		m.form.SetValues([]string{
			existing.Name,
			string(existing.Type),
			money.Format(existing.StartingBalanceCents),
			lim,
			apr,
			pay,
			include,
			catName,
		})
	} else {
		m.form.fields[1].input.SetValue(string(store.TypeChecking))
		m.form.fields[6].input.SetValue("no")
	}
	m.form.Focus()
	m.mode = acctForm
}

func (m accountsModel) updateForm(msg tea.Msg) (accountsModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		// Open type picker on Type field with space key.
		if m.form.focus == 1 && (km.String() == " " || km.String() == "ctrl+t") {
			items := make([]string, 0, len(store.AllAccountTypes()))
			cur := 0
			for i, t := range store.AllAccountTypes() {
				items = append(items, string(t))
				if string(t) == m.form.fields[1].input.Value() {
					cur = i
				}
			}
			m.picker = newPicker("Account type", items, cur)
			m.mode = acctTypePick
			return m, nil
		}
		// Open category picker on payment-category field.
		if m.form.focus == 7 && (km.String() == " " || km.String() == "ctrl+t") {
			items := []string{"(none)"}
			cur := 0
			currentName := m.form.fields[7].input.Value()
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
			m.picker = newPicker("Payment category", items, cur)
			m.mode = acctCategoryPick
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = acctList
		return m, cmd
	}
	if m.form.submitted {
		return m.save()
	}
	return m, cmd
}

func (m accountsModel) updateCategoryPicker(msg tea.Msg) (accountsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = acctForm
		return m, cmd
	}
	if m.picker.chosen {
		if m.picker.cursor == 0 {
			m.form.fields[7].input.SetValue("")
		} else {
			m.form.fields[7].input.SetValue(stripGroupPrefix(m.picker.items[m.picker.cursor]))
		}
		m.mode = acctForm
	}
	return m, cmd
}

func (m accountsModel) updateTypePicker(msg tea.Msg) (accountsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = acctForm
		return m, cmd
	}
	if m.picker.chosen {
		m.form.fields[1].input.SetValue(m.picker.items[m.picker.cursor])
		m.mode = acctForm
		return m, cmd
	}
	return m, cmd
}

func (m accountsModel) updateConfirm(msg tea.Msg) (accountsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	if m.confirm.answered {
		if m.confirm.yes && len(m.rows) > 0 {
			id := m.rows[m.cursor].ID
			if err := m.store.ArchiveAccount(context.Background(), id); err != nil {
				cmd = tea.Batch(cmd, flashFail("archive: "+err.Error()))
			} else {
				cmd = tea.Batch(cmd, flashOK("Archived"))
			}
			cmd = tea.Batch(cmd, m.Refresh())
		}
		m.mode = acctList
	}
	return m, cmd
}

func (m accountsModel) save() (accountsModel, tea.Cmd) {
	vals := m.form.Values()
	if vals[0] == "" {
		m.form.err = "name is required"
		return m, nil
	}
	t := store.AccountType(vals[1])
	valid := false
	for _, v := range store.AllAccountTypes() {
		if v == t {
			valid = true
		}
	}
	if !valid {
		m.form.err = "invalid type"
		return m, nil
	}
	startCents := int64(0)
	if vals[2] != "" {
		c, err := money.Parse(vals[2])
		if err != nil {
			m.form.err = "starting balance: " + err.Error()
			return m, nil
		}
		startCents = c
	}
	var limPtr *int64
	if vals[3] != "" {
		c, err := money.Parse(vals[3])
		if err != nil {
			m.form.err = "credit limit: " + err.Error()
			return m, nil
		}
		limPtr = &c
	}
	var aprPtr *int64
	if vals[4] != "" {
		var f float64
		if _, err := fmt.Sscanf(vals[4], "%f", &f); err != nil {
			m.form.err = "APR: invalid number"
			return m, nil
		}
		bps := int64(f * 100)
		aprPtr = &bps
	}

	var payPtr *int64
	if vals[5] != "" {
		c, err := money.Parse(vals[5])
		if err != nil {
			m.form.err = "monthly payment: " + err.Error()
			return m, nil
		}
		payPtr = &c
	}
	include := strings.EqualFold(strings.TrimSpace(vals[6]), "yes") ||
		strings.EqualFold(strings.TrimSpace(vals[6]), "y") ||
		strings.EqualFold(strings.TrimSpace(vals[6]), "true")

	var payCatPtr *int64
	if vals[7] != "" {
		for _, c := range m.cats {
			if c.Name == vals[7] {
				cid := c.ID
				payCatPtr = &cid
				break
			}
		}
	}

	// Liabilities owe money. The user's mental model is to enter a positive
	// "amount owed", but internally we store it as a negative balance so
	// that ledger math (balance = start + Σinflow − Σoutflow) yields a less
	// negative number as the debt is paid down.
	if (t == store.TypeCredit || t == store.TypeLoan) && startCents > 0 {
		startCents = -startCents
	}

	a := store.Account{
		Name:                 vals[0],
		Type:                 t,
		StartingBalanceCents: startCents,
		CreditLimitCents:     limPtr,
		AprBps:               aprPtr,
		MonthlyPaymentCents:  payPtr,
		IncludeInPaydown:     include,
		PaymentCategoryID:    payCatPtr,
	}
	if m.editing != nil {
		a.ID = m.editing.ID
		if err := m.store.UpdateAccount(context.Background(), a); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
	} else {
		if _, err := m.store.CreateAccount(context.Background(), a); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
	}
	m.mode = acctList
	return m, tea.Batch(m.Refresh(), flashOK("Saved"))
}

func (m accountsModel) View() string {
	switch m.mode {
	case acctForm:
		title := "New account"
		if m.editing != nil {
			title = "Edit account"
		}
		return m.form.View(title)
	case acctTypePick, acctCategoryPick:
		return m.picker.View()
	case acctConfirmDel:
		return m.confirm.View()
	}
	return m.viewList()
}

func (m accountsModel) viewList() string {
	if len(m.rows) == 0 {
		return styleDim.Render("No accounts yet. Press n to add your first one.")
	}
	headers := []string{"Name", "Type", "Balance", "Limit", "APR", "Available"}
	widths := []int{24, 10, 14, 14, 10, 14}
	rows := make([][]string, len(m.rows))
	for i, r := range m.rows {
		bal := money.Format(r.BalanceCents)
		if r.BalanceCents < 0 {
			bal = styleNeg.Render(bal)
		} else if r.BalanceCents > 0 {
			bal = stylePos.Render(bal)
		}
		limit := ""
		if r.CreditLimitCents != nil {
			limit = money.Format(*r.CreditLimitCents)
		}
		apr := ""
		if r.AprBps != nil {
			apr = fmt.Sprintf("%.2f%%", float64(*r.AprBps)/100.0)
		}
		avail := ""
		if r.CreditLimitCents != nil {
			a := availableCredit(r)
			avail = stylePos.Render(money.Format(a))
		}
		rows[i] = []string{r.Name, string(r.Type), bal, limit, apr, avail}
	}
	body := renderTable(headers, widths, rows, m.cursor, "acct-row-")

	// Net-worth summary: assets are non-liability accounts; liabilities are
	// credit + loan accounts (which carry negative balances when in debt).
	var assets, liabs int64
	for _, r := range m.rows {
		if r.Type.IsLiability() {
			liabs += r.BalanceCents
		} else {
			assets += r.BalanceCents
		}
	}
	diff := assets + liabs

	// Sum available credit across every account that has a limit set.
	// `balance + limit` works for both directions: a credit card with a
	// $5,000 limit and −$1,000 balance has $4,000 left to spend; a checking
	// account with a $1,000 overdraft limit and a $500 balance can draw up
	// to $1,500 before hitting the floor.
	var availCredit int64
	for _, r := range m.rows {
		if a := availableCredit(r); a > 0 {
			availCredit += a
		}
	}

	labelW := 20
	summaryLines := []string{
		"  " + padRight(styleHeader.Render("Assets:"), labelW) + stylePos.Render(money.Format(assets)),
		"  " + padRight(styleHeader.Render("Liabilities:"), labelW) + styleNeg.Render(money.Format(liabs)),
		"  " + padRight(styleHeader.Render("Difference:"), labelW) + diffColored(diff),
	}
	if availCredit > 0 {
		summaryLines = append(summaryLines,
			"  "+padRight(styleHeader.Render("Available credit:"), labelW)+stylePos.Render(money.Format(availCredit)))
	}
	summary := strings.Join(summaryLines, "\n")

	return strings.Join([]string{styleTitle.Render("Accounts"), body, summary}, "\n")
}

// availableCredit reports how much room is left under an account's limit.
// Returns 0 for accounts with no limit set.
func availableCredit(a store.AccountWithBalance) int64 {
	if a.CreditLimitCents == nil {
		return 0
	}
	v := a.BalanceCents + *a.CreditLimitCents
	if v < 0 {
		return 0
	}
	return v
}

func diffColored(cents int64) string {
	s := money.Format(cents)
	switch {
	case cents > 0:
		return stylePos.Render(s)
	case cents < 0:
		return styleNeg.Render(s)
	}
	return styleDim.Render(s)
}
