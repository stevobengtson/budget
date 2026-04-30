package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/barchart"
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sbengtson/budget/internal/money"
	"github.com/sbengtson/budget/internal/store"
)

type rptMode int

const (
	rptSpending rptMode = iota
	rptCashflow
)

type rptPeriod int

const (
	rptThisMonth rptPeriod = iota
	rpt30Day
	rpt90Day
	rptYTD
)

func (p rptPeriod) Label() string {
	switch p {
	case rptThisMonth:
		return "This month"
	case rpt30Day:
		return "Last 30 days"
	case rpt90Day:
		return "Last 90 days"
	case rptYTD:
		return "Year to date"
	}
	return ""
}

func (p rptPeriod) Range(now time.Time) (time.Time, time.Time) {
	switch p {
	case rptThisMonth:
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1)
		return first, last
	case rpt30Day:
		return now.AddDate(0, 0, -29), now
	case rpt90Day:
		return now.AddDate(0, 0, -89), now
	case rptYTD:
		first := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return first, now
	}
	return now, now
}

type reportsModel struct {
	store *store.Store

	mode   rptMode
	period rptPeriod

	width, height int

	spend []store.CategorySpend
	cash  []store.MonthCashflow

	spendPager paginator.Model
}

const rptSpendPageSize = 10

func newReportsModel(s *store.Store) reportsModel {
	pg := paginator.New()
	pg.Type = paginator.Arabic
	pg.PerPage = rptSpendPageSize
	return reportsModel{store: s, mode: rptSpending, period: rptThisMonth, spendPager: pg}
}

func (m reportsModel) modal() bool { return false }

func (m *reportsModel) Refresh() tea.Cmd {
	ctx := context.Background()
	now := time.Now()

	since, until := m.period.Range(now)
	spend, err := m.store.SpendingByCategory(ctx, since, until)
	if err != nil {
		return flashFail("spending: " + err.Error())
	}
	m.spend = spend
	m.spendPager.SetTotalPages(len(m.spend))
	if m.spendPager.Page >= m.spendPager.TotalPages {
		m.spendPager.Page = max0(m.spendPager.TotalPages - 1)
	}

	cash, err := m.store.MonthlyCashflow(ctx, 12)
	if err != nil {
		return flashFail("cashflow: " + err.Error())
	}
	m.cash = cash
	return nil
}

func (m *reportsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *reportsModel) HandleMouse(msg tea.MouseMsg) tea.Cmd { return nil }

func (m reportsModel) Update(msg tea.Msg) (reportsModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "s":
			m.mode = rptSpending
		case "c":
			m.mode = rptCashflow
		case "[":
			if m.period > 0 {
				m.period--
				return m, m.Refresh()
			}
		case "]":
			if m.period < rptYTD {
				m.period++
				return m, m.Refresh()
			}
		case "pgdown", "pgdn":
			if m.mode == rptSpending {
				m.spendPager.NextPage()
			}
		case "pgup":
			if m.mode == rptSpending {
				m.spendPager.PrevPage()
			}
		case "home":
			if m.mode == rptSpending {
				m.spendPager.Page = 0
			}
		case "end":
			if m.mode == rptSpending {
				m.spendPager.Page = max0(m.spendPager.TotalPages - 1)
			}
		case "r":
			return m, m.Refresh()
		}
	}
	return m, nil
}

func (m reportsModel) View() string {
	switch m.mode {
	case rptSpending:
		return m.viewSpending()
	case rptCashflow:
		return m.viewCashflow()
	}
	return ""
}

// chartArea returns the (w, h) the bar chart should use.
func (m reportsModel) chartArea() (int, int) {
	w := m.width - 4
	h := m.height - 12 // tab bar + status bar + title + banner + help
	if w < 40 {
		w = 40
	}
	if h < 8 {
		h = 8
	}
	if w > 160 {
		w = 160
	}
	if h > 36 {
		h = 36
	}
	return w, h
}

var (
	rptBlockExpense = lipgloss.NewStyle().
			Foreground(colorBad).
			Background(colorBad)
	rptBlockIncome = lipgloss.NewStyle().
			Foreground(colorOK).
			Background(colorOK)
	rptBlockNet = lipgloss.NewStyle().
			Foreground(colorHeading).
			Background(colorHeading)
	rptAxis  = lipgloss.NewStyle().Foreground(colorMuted)
	rptLabel = lipgloss.NewStyle().Foreground(colorAccent)
)

func (m reportsModel) viewSpending() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Reports · Spending by category"))
	b.WriteString("\n")

	since, until := m.period.Range(time.Now())
	period := fmt.Sprintf("  %s · %s — %s",
		styleDim.Render("["+m.period.Label()+"]"),
		since.Format("Jan 2, 2006"),
		until.Format("Jan 2, 2006"),
	)
	b.WriteString(period)
	b.WriteString("\n\n")

	if len(m.spend) == 0 {
		b.WriteString(styleDim.Render("No spending in this period."))
		b.WriteString("\n\n")
			return b.String()
	}

	// Total across the entire period (for percentage denominators), then
	// slice to the current page for the chart + table.
	var total int64
	for _, r := range m.spend {
		total += r.OutflowCents
	}
	start, end := m.spendPager.GetSliceBounds(len(m.spend))
	page := m.spend[start:end]

	values := make([]barchart.BarData, 0, len(page))
	for _, r := range page {
		values = append(values, barchart.BarData{
			Label: truncate(r.CategoryName, 12),
			Values: []barchart.BarValue{{
				Name:  r.CategoryName,
				Value: float64(r.OutflowCents) / 100.0,
				Style: rptBlockExpense,
			}},
		})
	}

	// Compact chart sized to current page only. Each bar = 1 row + 1 gap.
	w, _ := m.chartArea()
	gap := 1
	barW := 1
	needed := len(values)*barW + (len(values)-1)*gap + 2
	if needed < 6 {
		needed = 6
	}

	if len(values) > 0 {
		// Lock the chart's MaxValue to the period's largest spend so bar
		// widths stay comparable across pages.
		max := float64(m.spend[0].OutflowCents) / 100.0
		chart := barchart.New(w, needed,
			barchart.WithHorizontalBars(),
			barchart.WithDataSet(values),
			barchart.WithStyles(rptAxis, rptLabel),
			barchart.WithBarWidth(barW),
			barchart.WithBarGap(gap),
			barchart.WithNoAutoBarWidth(),
			barchart.WithNoAutoMaxValue(),
			barchart.WithMaxValue(max),
		)
		chart.Draw()
		b.WriteString(chart.View())
		b.WriteString("\n")
	}

	// Numeric breakdown for the current page only.
	b.WriteString("\n")
	for _, r := range page {
		pct := float64(r.OutflowCents) / float64(total) * 100.0
		line := fmt.Sprintf("  %-20s  %12s  %5.1f%%  %s",
			r.CategoryName,
			styleNeg.Render(money.Format(r.OutflowCents)),
			pct,
			styleDim.Render(r.GroupName),
		)
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s %s   %s\n",
		styleHeader.Render("Total"),
		styleNeg.Render(money.Format(total)),
		styleDim.Render(fmt.Sprintf("(%d categories)", len(m.spend)))))

	if m.spendPager.TotalPages > 1 {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s %s · rows %d-%d of %d · pgup/pgdn\n",
			styleDim.Render("page"),
			m.spendPager.View(),
			start+1, end, len(m.spend)))
	}

	b.WriteString("\n")
	return b.String()
}

func (m reportsModel) viewCashflow() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Reports · Monthly cashflow"))
	b.WriteString("\n")
	b.WriteString("  " + styleDim.Render("12 months · income (green) vs expense (red)"))
	b.WriteString("\n\n")

	if len(m.cash) == 0 {
		b.WriteString(styleDim.Render("No cashflow data yet."))
		b.WriteString("\n\n")
			return b.String()
	}

	w, h := m.chartArea()
	values := make([]barchart.BarData, 0, len(m.cash))
	var totalIn, totalOut int64
	for _, r := range m.cash {
		// Use actual transaction inflow if non-zero, else fall back to
		// configured income for the month so future projections still
		// render a green bar.
		in := r.InflowCents
		if in == 0 {
			in = r.IncomeCents
		}
		totalIn += in
		totalOut += r.OutflowCents
		t, _ := time.Parse("2006-01", r.Month)
		values = append(values, barchart.BarData{
			Label: t.Format("Jan"),
			Values: []barchart.BarValue{
				{Name: "Income", Value: float64(in) / 100.0, Style: rptBlockIncome},
				{Name: "Expense", Value: float64(r.OutflowCents) / 100.0, Style: rptBlockExpense},
			},
		})
	}

	chart := barchart.New(w, h,
		barchart.WithDataSet(values),
		barchart.WithStyles(rptAxis, rptLabel),
		barchart.WithBarGap(1),
	)
	chart.Draw()
	b.WriteString(chart.View())
	b.WriteString("\n\n")

	// Totals + per-month detail
	b.WriteString(fmt.Sprintf("  %s %s   %s %s   %s %s\n",
		styleDim.Render("Income (12m)"), stylePos.Render(money.Format(totalIn)),
		styleDim.Render("Expense (12m)"), styleNeg.Render(money.Format(totalOut)),
		styleDim.Render("Net"), netColored(totalIn-totalOut),
	))
	b.WriteString("\n")
	b.WriteString("  " + styleHeader.Render(padRight("Month", 10)) +
		styleHeader.Render(padRight("Income", 14)) +
		styleHeader.Render(padRight("Expense", 14)) +
		styleHeader.Render(padRight("Net", 14)))
	b.WriteString("\n")
	for _, r := range m.cash {
		t, _ := time.Parse("2006-01", r.Month)
		in := r.InflowCents
		if in == 0 {
			in = r.IncomeCents
		}
		net := in - r.OutflowCents
		b.WriteString("  " +
			padRight(t.Format("Jan 2006"), 10) +
			padRight(stylePos.Render(money.Format(in)), 14) +
			padRight(styleNeg.Render(money.Format(r.OutflowCents)), 14) +
			padRight(netColored(net), 14))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return b.String()
}

func netColored(cents int64) string {
	s := money.Format(cents)
	switch {
	case cents > 0:
		return stylePos.Render(s)
	case cents < 0:
		return styleNeg.Render(s)
	}
	return styleDim.Render(s)
}


