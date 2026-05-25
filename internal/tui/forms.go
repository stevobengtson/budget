package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// field is one labelled input in a form.
type field struct {
	label string
	input textinput.Model
	hint  string // optional helper text
}

func newField(label, placeholder, hint string) field {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 200
	ti.Prompt = "  "
	return field{label: label, input: ti, hint: hint}
}

// form is a stack of fields plus optional pickers, navigated with tab/shift-tab.
type form struct {
	fields    []field
	focus     int
	err       string
	submitted bool
	canceled  bool
	subtitle  string // optional dim info line rendered under title
}

func (f *form) Focus() {
	for i := range f.fields {
		if i == f.focus {
			f.fields[i].input.Focus()
		} else {
			f.fields[i].input.Blur()
		}
	}
}

func (f *form) SetValues(vals []string) {
	for i, v := range vals {
		if i < len(f.fields) {
			f.fields[i].input.SetValue(v)
		}
	}
}

func (f *form) Values() []string {
	out := make([]string, len(f.fields))
	for i, fd := range f.fields {
		out[i] = strings.TrimSpace(fd.input.Value())
	}
	return out
}

func (f *form) Update(msg tea.Msg) (form, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.Type {
		case tea.KeyEsc:
			f.canceled = true
			return *f, nil
		case tea.KeyTab, tea.KeyDown:
			f.focus = (f.focus + 1) % len(f.fields)
			f.Focus()
			return *f, nil
		case tea.KeyShiftTab, tea.KeyUp:
			f.focus = (f.focus - 1 + len(f.fields)) % len(f.fields)
			f.Focus()
			return *f, nil
		case tea.KeyEnter:
			if f.focus < len(f.fields)-1 {
				f.focus++
				f.Focus()
				return *f, nil
			}
			f.submitted = true
			return *f, nil
		}
	}
	var cmd tea.Cmd
	f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(msg)
	return *f, cmd
}

func (f *form) View(title string) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(title))
	b.WriteString("\n")
	if f.subtitle != "" {
		b.WriteString(f.subtitle)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for i, fd := range f.fields {
		label := fd.label
		if i == f.focus {
			label = styleSelected.Render("▸ " + label)
		} else {
			label = styleDim.Render("  " + label)
		}
		b.WriteString(label)
		b.WriteString("\n")
		b.WriteString(fd.input.View())
		if fd.hint != "" {
			b.WriteString(" " + styleDim.Render(fd.hint))
		}
		b.WriteString("\n\n")
	}
	if f.err != "" {
		b.WriteString(styleErr.Render(f.err))
		b.WriteString("\n")
	}
	b.WriteString(styleHelp.Render("enter: next/save · tab/↑↓: move · esc: cancel"))
	return stylePanel.Render(b.String())
}

// picker is a simple modal list selector.
type picker struct {
	title    string
	items    []string
	cursor   int
	chosen   bool
	canceled bool
}

func newPicker(title string, items []string, current int) picker {
	if current < 0 || current >= len(items) {
		current = 0
	}
	return picker{title: title, items: items, cursor: current}
}

func (p *picker) Update(msg tea.Msg) (picker, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.Type {
		case tea.KeyEsc:
			p.canceled = true
		case tea.KeyDown:
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case tea.KeyUp:
			if p.cursor > 0 {
				p.cursor--
			}
		case tea.KeyEnter:
			p.chosen = true
		}
		switch km.String() {
		case "j":
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case "k":
			if p.cursor > 0 {
				p.cursor--
			}
		}
	}
	return *p, nil
}

func (p *picker) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(p.title))
	b.WriteString("\n\n")
	for i, it := range p.items {
		if i == p.cursor {
			b.WriteString(styleSelected.Render("▸ " + it))
		} else {
			b.WriteString("  " + it)
		}
		b.WriteString("\n")
	}
	b.WriteString(styleHelp.Render("\n↑↓: move · enter: select · esc: cancel"))
	return stylePanel.Render(b.String())
}

// confirmModel asks yes/no.
type confirmModel struct {
	prompt   string
	answered bool
	yes      bool
}

func (c *confirmModel) Update(msg tea.Msg) (confirmModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y":
			c.answered, c.yes = true, true
		case "n", "N", "esc":
			c.answered, c.yes = true, false
		}
	}
	return *c, nil
}

func (c *confirmModel) View() string {
	body := c.prompt + "\n\n" + styleDim.Render("y: yes  ·  n/esc: no")
	return stylePanel.BorderForeground(colorWarn).Render(body)
}

// renderTable left-pads cells to widths and joins rows. If zonePrefix is
// non-empty, each row line is wrapped in a bubblezone mark of the form
// "<prefix><i>" so callers can resolve mouse clicks to row indices.
func renderTable(headers []string, widths []int, rows [][]string, cursor int, zonePrefix string) string {
	var b strings.Builder
	hdrCells := make([]string, len(headers))
	for i, h := range headers {
		hdrCells[i] = styleHeader.Render(padRight(h, widths[i]))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, hdrCells...))
	b.WriteString("\n")
	for ri, row := range rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = padRight(c, widths[i])
		}
		line := lipgloss.JoinHorizontal(lipgloss.Top, cells...)
		if ri == cursor {
			line = styleSelected.Render("▸ ") + line
		} else {
			line = "  " + line
		}
		if zonePrefix != "" {
			line = zone.Mark(zonePrefix+strconv.Itoa(ri), line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// stripGroupPrefix turns "Group · Category" into "Category". The separator
// " · " is 4 bytes (the middle dot is U+00B7, 2 bytes UTF-8) — using a literal
// `i+3` slice silently leaves a leading space and breaks downstream
// name-based lookups.
func stripGroupPrefix(label string) string {
	const sep = " · "
	if i := strings.Index(label, sep); i >= 0 {
		return label[i+len(sep):]
	}
	return label
}

func padRight(s string, w int) string {
	if lipgloss.Width(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}

// renderTableWithStartIndex is renderTable with a zone-id offset, so clicks
// on a paged slice resolve to absolute row indices in the source data.
func renderTableWithStartIndex(headers []string, widths []int, rows [][]string, cursor int, zonePrefix string, startIdx int) string {
	var b strings.Builder
	hdrCells := make([]string, len(headers))
	for i, h := range headers {
		hdrCells[i] = styleHeader.Render(padRight(h, widths[i]))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, hdrCells...))
	b.WriteString("\n")
	for ri, row := range rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = padRight(c, widths[i])
		}
		line := lipgloss.JoinHorizontal(lipgloss.Top, cells...)
		if ri == cursor {
			line = styleSelected.Render("▸ ") + line
		} else {
			line = "  " + line
		}
		if zonePrefix != "" {
			line = zone.Mark(zonePrefix+strconv.Itoa(startIdx+ri), line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// truncate shortens a plain string to at most n display columns, appending an
// ellipsis. Use only on raw text — does not respect ANSI escape sequences.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
