package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/store"
)

type catMode int

const (
	catList catMode = iota
	catKindPick    // pick: new group | new category
	catGroupForm
	catCatForm
	catGroupPick
	catConfirmDel
)

type catRow struct {
	isGroup  bool
	groupID  int64
	groupName string
	cat      *store.Category
}

type categoriesModel struct {
	store     *store.Store
	groups    []store.CategoryGroup
	cats      []store.Category
	rows      []catRow
	cursor    int
	mode      catMode
	form      form
	picker    picker
	confirm   confirmModel
	editing   *store.Category
	editingGrp *store.CategoryGroup

	width, height int
	scrollOffset  int
}

func newCategoriesModel(s *store.Store) categoriesModel { return categoriesModel{store: s} }

func (m *categoriesModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.adjustScroll()
}

// linesAvailable returns rows allowed for the category list (rows are 1
// line each: group headers and categories alike).
func (m categoriesModel) linesAvailable() int {
	// chrome: tab bar (3) + title (1) + blank (1) + ↑more (1) + ↓more (1)
	// + blank (1) + status (1) + safety (3)
	chrome := 12
	avail := m.height - chrome
	if avail < 5 {
		avail = 5
	}
	return avail
}

func (m *categoriesModel) adjustScroll() {
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
	avail := m.linesAvailable()
	if m.scrollOffset > m.cursor {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+avail {
		m.scrollOffset = m.cursor - avail + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m categoriesModel) modal() bool { return m.mode != catList }

func (m *categoriesModel) HandleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != catList {
		return nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	for i := range m.rows {
		if zone.Get("cat-row-" + strconv.Itoa(i)).InBounds(msg) {
			m.cursor = i
			return nil
		}
	}
	return nil
}

func (m categoriesModel) Init() tea.Cmd { return m.Refresh() }

func (m *categoriesModel) Refresh() tea.Cmd {
	gs, err := m.store.ListGroups(context.Background())
	if err != nil {
		return flashFail("groups: " + err.Error())
	}
	cs, err := m.store.ListCategories(context.Background(), false)
	if err != nil {
		return flashFail("categories: " + err.Error())
	}
	m.groups = gs
	m.cats = cs

	rows := make([]catRow, 0, len(gs)+len(cs))
	for _, g := range gs {
		rows = append(rows, catRow{isGroup: true, groupID: g.ID, groupName: g.Name})
		for i := range cs {
			if cs[i].GroupID == g.ID {
				c := cs[i]
				rows = append(rows, catRow{groupID: g.ID, groupName: g.Name, cat: &c})
			}
		}
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = max0(len(rows) - 1)
	}
	m.adjustScroll()
	return nil
}

func (m categoriesModel) Update(msg tea.Msg) (categoriesModel, tea.Cmd) {
	switch m.mode {
	case catList:
		return m.updateList(msg)
	case catKindPick:
		return m.updateKindPicker(msg)
	case catGroupForm:
		return m.updateGroupForm(msg)
	case catCatForm:
		return m.updateCatForm(msg)
	case catGroupPick:
		return m.updateGroupPicker(msg)
	case catConfirmDel:
		return m.updateConfirm(msg)
	}
	return m, nil
}

func (m categoriesModel) updateList(msg tea.Msg) (categoriesModel, tea.Cmd) {
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
			avail := m.linesAvailable()
			m.cursor -= avail
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.adjustScroll()
		case "pgdown", "pgdn", "ctrl+d":
			avail := m.linesAvailable()
			m.cursor += avail
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
		case "n":
			items := []string{"New group", "New category"}
			m.picker = newPicker("Add what?", items, 0)
			m.mode = catKindPick
		case "enter":
			if len(m.rows) == 0 {
				return m, nil
			}
			r := m.rows[m.cursor]
			if r.isGroup {
				g := m.groups[indexGroup(m.groups, r.groupID)]
				m.editingGrp = &g
				m.startGroupForm(&g)
			} else if r.cat.IsIncome {
				return m, flashFail("Income is system-managed; cannot be edited")
			} else {
				c := *r.cat
				m.editing = &c
				m.startCatForm(&c)
			}
		case "d":
			if len(m.rows) == 0 {
				return m, nil
			}
			r := m.rows[m.cursor]
			if !r.isGroup && r.cat.IsIncome {
				return m, flashFail("Income is system-managed; cannot be deleted")
			}
			if r.isGroup {
				m.confirm = confirmModel{prompt: fmt.Sprintf("Delete group %q? Only allowed if it has no categories.", r.groupName)}
			} else {
				m.confirm = confirmModel{prompt: fmt.Sprintf("Archive category %q?", r.cat.Name)}
			}
			m.mode = catConfirmDel
		}
	}
	return m, nil
}

func indexGroup(groups []store.CategoryGroup, id int64) int {
	for i, g := range groups {
		if g.ID == id {
			return i
		}
	}
	return 0
}

func (m *categoriesModel) startGroupForm(existing *store.CategoryGroup) {
	m.editingGrp = existing
	m.editing = nil
	m.form = form{fields: []field{newField("Group name", "Monthly", "")}}
	if existing != nil {
		m.form.SetValues([]string{existing.Name})
	}
	m.form.Focus()
	m.mode = catGroupForm
}

func (m *categoriesModel) startCatForm(existing *store.Category) {
	m.editing = existing
	m.editingGrp = nil
	groupName := ""
	if existing != nil {
		for _, g := range m.groups {
			if g.ID == existing.GroupID {
				groupName = g.Name
				break
			}
		}
	} else if len(m.groups) > 0 {
		groupName = m.groups[0].Name
	}
	fields := []field{
		newField("Group", groupName, "press space to pick"),
		newField("Name", "Groceries", ""),
		newField("Goal amount", "", "blank for no goal"),
		newField("Goal due date", "YYYY-MM-DD", "blank if N/A"),
	}
	m.form = form{fields: fields}
	m.form.fields[0].input.SetValue(groupName)
	if existing != nil {
		var goal, due string
		if existing.GoalCents != nil {
			goal = money.Format(*existing.GoalCents)
		}
		if existing.GoalDueDate != nil {
			due = existing.GoalDueDate.Format("2006-01-02")
		}
		m.form.SetValues([]string{groupName, existing.Name, goal, due})
	}
	m.form.Focus()
	m.mode = catCatForm
}

func (m categoriesModel) updateKindPicker(msg tea.Msg) (categoriesModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = catList
		return m, cmd
	}
	if m.picker.chosen {
		if m.picker.cursor == 0 {
			m.startGroupForm(nil)
		} else {
			if len(m.groups) == 0 {
				m.mode = catList
				return m, flashFail("create a group first")
			}
			m.startCatForm(nil)
		}
	}
	return m, cmd
}

func (m categoriesModel) updateGroupForm(msg tea.Msg) (categoriesModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = catList
		return m, cmd
	}
	if m.form.submitted {
		name := m.form.Values()[0]
		if name == "" {
			m.form.err = "name required"
			return m, cmd
		}
		if m.editingGrp != nil {
			err := m.store.UpdateGroup(context.Background(), store.CategoryGroup{ID: m.editingGrp.ID, Name: name})
			if err != nil {
				m.form.err = err.Error()
				return m, cmd
			}
		} else {
			if _, err := m.store.CreateGroup(context.Background(), name, int64(len(m.groups))); err != nil {
				m.form.err = err.Error()
				return m, cmd
			}
		}
		m.mode = catList
		return m, tea.Batch(m.Refresh(), flashOK("Saved"))
	}
	return m, cmd
}

func (m categoriesModel) updateCatForm(msg tea.Msg) (categoriesModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		if m.form.focus == 0 && (km.String() == " " || km.String() == "ctrl+t") {
			items := make([]string, len(m.groups))
			cur := 0
			currentName := m.form.fields[0].input.Value()
			for i, g := range m.groups {
				items[i] = g.Name
				if g.Name == currentName {
					cur = i
				}
			}
			m.picker = newPicker("Group", items, cur)
			m.mode = catGroupPick
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	if m.form.canceled {
		m.mode = catList
		return m, cmd
	}
	if m.form.submitted {
		return m.saveCat()
	}
	return m, cmd
}

func (m categoriesModel) updateGroupPicker(msg tea.Msg) (categoriesModel, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	if m.picker.canceled {
		m.mode = catCatForm
		return m, cmd
	}
	if m.picker.chosen {
		m.form.fields[0].input.SetValue(m.picker.items[m.picker.cursor])
		m.mode = catCatForm
	}
	return m, cmd
}

func (m categoriesModel) saveCat() (categoriesModel, tea.Cmd) {
	vals := m.form.Values()
	if vals[1] == "" {
		m.form.err = "name required"
		return m, nil
	}
	var groupID int64
	for _, g := range m.groups {
		if g.Name == vals[0] {
			groupID = g.ID
			break
		}
	}
	if groupID == 0 {
		m.form.err = "select a group"
		return m, nil
	}
	var goalPtr *int64
	if vals[2] != "" {
		c, err := money.Parse(vals[2])
		if err != nil {
			m.form.err = "goal: " + err.Error()
			return m, nil
		}
		goalPtr = &c
	}
	var duePtr *time.Time
	if vals[3] != "" {
		t, err := time.Parse("2006-01-02", vals[3])
		if err != nil {
			m.form.err = "due date: " + err.Error()
			return m, nil
		}
		duePtr = &t
	}
	c := store.Category{
		GroupID:     groupID,
		Name:        vals[1],
		GoalCents:   goalPtr,
		GoalDueDate: duePtr,
	}
	if m.editing != nil {
		c.ID = m.editing.ID
		if err := m.store.UpdateCategory(context.Background(), c); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
	} else {
		if _, err := m.store.CreateCategory(context.Background(), c); err != nil {
			m.form.err = err.Error()
			return m, nil
		}
	}
	m.mode = catList
	return m, tea.Batch(m.Refresh(), flashOK("Saved"))
}

func (m categoriesModel) updateConfirm(msg tea.Msg) (categoriesModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	if m.confirm.answered {
		if m.confirm.yes && len(m.rows) > 0 {
			r := m.rows[m.cursor]
			ctx := context.Background()
			var err error
			if r.isGroup {
				err = m.store.DeleteGroup(ctx, r.groupID)
			} else {
				err = m.store.ArchiveCategory(ctx, r.cat.ID)
			}
			if err != nil {
				cmd = tea.Batch(cmd, flashFail(err.Error()))
			} else {
				cmd = tea.Batch(cmd, flashOK("Done"))
			}
			cmd = tea.Batch(cmd, m.Refresh())
		}
		m.mode = catList
	}
	return m, cmd
}

func (m categoriesModel) View() string {
	switch m.mode {
	case catKindPick, catGroupPick:
		return m.picker.View()
	case catGroupForm:
		title := "New group"
		if m.editingGrp != nil {
			title = "Edit group"
		}
		return m.form.View(title)
	case catCatForm:
		title := "New category"
		if m.editing != nil {
			title = "Edit category"
		}
		return m.form.View(title)
	case catConfirmDel:
		return m.confirm.View()
	}
	return m.viewList()
}

func (m categoriesModel) viewList() string {
	if len(m.rows) == 0 {
		return styleDim.Render("No categories yet. Press n to add a group, then a category.")
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Categories"))
	b.WriteString("\n\n")

	avail := m.linesAvailable()
	start := m.scrollOffset
	if start < 0 {
		start = 0
	}
	end := start + avail
	if end > len(m.rows) {
		end = len(m.rows)
	}

	if start > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("  ↑ %d more above\n", start)))
	}

	for i := start; i < end; i++ {
		r := m.rows[i]
		marker := "  "
		if i == m.cursor {
			marker = styleSelected.Render("▸ ")
		}
		var line string
		if r.isGroup {
			line = marker + styleHeader.Render("["+r.groupName+"]")
		} else {
			extra := ""
			if r.cat.GoalCents != nil {
				extra = " · goal " + money.Format(*r.cat.GoalCents)
				if r.cat.GoalDueDate != nil {
					extra += " by " + r.cat.GoalDueDate.Format("2006-01-02")
				}
			}
			name := r.cat.Name
			if r.cat.IsIncome {
				name = stylePos.Render(name) + " " + styleDim.Render("🔒 system")
			}
			line = marker + "  " + name + styleDim.Render(extra)
		}
		b.WriteString(zone.Mark("cat-row-"+strconv.Itoa(i), line))
		b.WriteString("\n")
	}

	if end < len(m.rows) {
		b.WriteString(styleDim.Render(fmt.Sprintf("  ↓ %d more below\n", len(m.rows)-end)))
	}

	return b.String()
}
