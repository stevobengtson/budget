package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sbengtson/budget/internal/core/settings"
	"github.com/sbengtson/budget/internal/core/store"
)

type settingsModel struct {
	store *store.Store

	accounts []store.AccountWithBalance
	cats     []store.Category

	defaultAcct *int64 // nil = "use first"
	defaultCat  *int64

	picker       *picker
	pickerTarget int // 1=account, 2=category
}

func newSettingsModel(s *store.Store) settingsModel { return settingsModel{store: s} }

func (m *settingsModel) Refresh() error {
	ctx := context.Background()
	accs, err := m.store.ListAccounts(ctx, false)
	if err != nil {
		return err
	}
	cats, err := m.store.ListCategories(ctx, false)
	if err != nil {
		return err
	}
	m.accounts = accs
	m.cats = cats

	m.defaultAcct = nil
	m.defaultCat = nil
	if v, ok, _ := m.store.GetSetting(ctx, settings.DefaultAccountKey); ok && v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.defaultAcct = &id
		}
	}
	if v, ok, _ := m.store.GetSetting(ctx, settings.DefaultCategoryKey); ok && v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.defaultCat = &id
		}
	}
	return nil
}

func (m *settingsModel) modal() bool { return m.picker != nil }

func (m *settingsModel) setAccountID(id int64)  { v := id; m.defaultAcct = &v }
func (m *settingsModel) setCategoryID(id int64) { v := id; m.defaultCat = &v }

func (m *settingsModel) Reset() {
	m.defaultAcct = nil
	m.defaultCat = nil
}

// Save persists or clears the two settings according to current state.
func (m *settingsModel) Save() error {
	ctx := context.Background()
	if m.defaultAcct == nil {
		if err := m.store.DeleteSetting(ctx, settings.DefaultAccountKey); err != nil {
			return err
		}
	} else {
		if err := m.store.SetSetting(ctx, settings.DefaultAccountKey,
			strconv.FormatInt(*m.defaultAcct, 10)); err != nil {
			return err
		}
	}
	if m.defaultCat == nil {
		if err := m.store.DeleteSetting(ctx, settings.DefaultCategoryKey); err != nil {
			return err
		}
	} else {
		if err := m.store.SetSetting(ctx, settings.DefaultCategoryKey,
			strconv.FormatInt(*m.defaultCat, 10)); err != nil {
			return err
		}
	}
	return nil
}

// activeAccounts returns non-archived accounts for the picker.
func (m *settingsModel) activeAccounts() []store.AccountWithBalance {
	out := make([]store.AccountWithBalance, 0, len(m.accounts))
	for _, a := range m.accounts {
		if a.ArchivedAt != nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

// activeCategories returns non-archived, non-income categories for the picker.
func (m *settingsModel) activeCategories() []store.Category {
	out := make([]store.Category, 0, len(m.cats))
	for _, c := range m.cats {
		if c.ArchivedAt != nil || c.IsIncome {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (m settingsModel) Update(msg tea.Msg) (settingsModel, tea.Cmd) {
	if m.picker != nil {
		p, cmd := m.picker.Update(msg)
		m.picker = &p
		if m.picker.canceled {
			m.picker = nil
			return m, cmd
		}
		if m.picker.chosen {
			idx := m.picker.cursor
			switch m.pickerTarget {
			case 1:
				active := m.activeAccounts()
				if idx == 0 {
					m.defaultAcct = nil
				} else if idx-1 < len(active) {
					id := active[idx-1].ID
					m.defaultAcct = &id
				}
			case 2:
				active := m.activeCategories()
				if idx == 0 {
					m.defaultCat = nil
				} else if idx-1 < len(active) {
					id := active[idx-1].ID
					m.defaultCat = &id
				}
			}
			m.picker = nil
			return m, cmd
		}
		return m, cmd
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "a":
			items := []string{"(use first)"}
			for _, a := range m.activeAccounts() {
				items = append(items, a.Name)
			}
			p := newPicker("Default account", items, 0)
			m.picker = &p
			m.pickerTarget = 1
			return m, nil
		case "c":
			items := []string{"(use first)"}
			for _, c := range m.activeCategories() {
				items = append(items, c.Name)
			}
			p := newPicker("Default category", items, 0)
			m.picker = &p
			m.pickerTarget = 2
			return m, nil
		case "r":
			m.Reset()
			return m, nil
		case "s":
			if err := m.Save(); err != nil {
				return m, flashFail(err.Error())
			}
			return m, flashOK("settings saved")
		}
	}
	return m, nil
}

func (m settingsModel) View() string {
	if m.picker != nil {
		return m.picker.View()
	}
	acctLabel := "(use first)"
	if m.defaultAcct != nil {
		acctLabel = lookupAccount(m.accounts, *m.defaultAcct)
		if acctLabel == "" {
			acctLabel = fmt.Sprintf("(missing id %d)", *m.defaultAcct)
		}
	}
	catLabel := "(use first)"
	if m.defaultCat != nil {
		catLabel = lookupCategory(m.cats, *m.defaultCat)
		if catLabel == "" {
			catLabel = fmt.Sprintf("(missing id %d)", *m.defaultCat)
		}
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Settings"))
	b.WriteString("\n\n")
	b.WriteString(styleSelected.Render("Default account: "))
	b.WriteString(acctLabel)
	b.WriteString("\n")
	b.WriteString(styleSelected.Render("Default category: "))
	b.WriteString(catLabel)
	b.WriteString("\n\n")
	b.WriteString(styleDim.Render("a edit account · c edit category · r reset · s save"))
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func (m *settingsModel) HandleMouse(tea.MouseMsg) tea.Cmd { return nil }
