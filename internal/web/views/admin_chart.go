package views

import (
	"fmt"
	"math"
	"strings"

	"github.com/sbengtson/budget/internal/core/store"
)

// The signups chart is drawn as a plain inline SVG on a fixed coordinate grid,
// scaled to its container by the viewBox. No charting library is involved: the
// app ships a single embedded binary with no bundler, and one line series does
// not justify the dependency. Colours come from the theme's chart-* tokens, so
// the chart follows light/dark like everything else.
const (
	chartWidth   = 720
	chartHeight  = 200
	chartPadLeft = 40
	chartPadRght = 12
	chartPadTop  = 14
	chartPadBot  = 28
)

// ChartPoint is one plotted bucket: its position in SVG user units plus the text
// shown on the axis and in the point's hover tooltip.
type ChartPoint struct {
	X, Y   float64
	Label  string // x-axis label, empty when this bucket's label is skipped
	Value  int
	Title  string // native SVG <title> tooltip, e.g. "Aug 4 — 3 signups"
	Bucket int    // index, used for stable element keys
}

// GridLine is one horizontal reference line and its y-axis value.
type GridLine struct {
	Y     float64
	Value int
}

// SignupChart is everything the template needs to draw the chart, precomputed so
// the templ file contains no arithmetic.
type SignupChart struct {
	Points []ChartPoint
	Grid   []GridLine
	// Line is the polyline "points" attribute; Area is the path "d" for the
	// translucent fill beneath it.
	Line string
	Area string
	Max  int
	// Total is the sum across the window, shown as the headline number.
	Total int
	// Empty reports that no signups landed in the window, so the template can
	// show a message instead of a flat line pretending to be data.
	Empty bool
	Range string // "week", "month" or "year"
}

// SignupRange describes one selectable window on the dashboard chart: how many
// buckets to fetch, at what granularity, and how to label them.
type SignupRange struct {
	Key   string
	Label string
	Unit  store.SignupUnit
	// Buckets is how many points the window holds.
	Buckets int
	// LabelEvery draws an x-axis label on every nth bucket (1 = all of them),
	// which is what keeps 30 daily points from turning the axis into mush.
	LabelEvery int
	// TimeFormat is the Go layout for a bucket's axis label.
	TimeFormat string
}

// SignupRanges are the dashboard's three windows, in display order. Week and
// month are daily buckets; year switches to monthly so the series stays
// readable rather than cramming 365 points into 720 units.
var SignupRanges = []SignupRange{
	{Key: "week", Label: "Week", Unit: store.SignupDaily, Buckets: 7, LabelEvery: 1, TimeFormat: "Jan 2"},
	{Key: "month", Label: "Month", Unit: store.SignupDaily, Buckets: 30, LabelEvery: 5, TimeFormat: "Jan 2"},
	{Key: "year", Label: "Year", Unit: store.SignupMonthly, Buckets: 12, LabelEvery: 1, TimeFormat: "Jan"},
}

// LookupSignupRange resolves a range key from the query string, falling back to
// the first range for anything unrecognised. Returning a value from this fixed
// set is also what keeps the caller from passing an arbitrary string into
// date_trunc.
func LookupSignupRange(key string) SignupRange {
	for _, r := range SignupRanges {
		if r.Key == key {
			return r
		}
	}
	return SignupRanges[0]
}

// BuildSignupChart turns a zero-filled bucket series into drawable geometry.
func BuildSignupChart(buckets []store.SignupBucket, r SignupRange) SignupChart {
	c := SignupChart{Range: r.Key, Empty: true}
	if len(buckets) == 0 {
		return c
	}

	max := 0
	for _, b := range buckets {
		c.Total += b.Count
		if b.Count > max {
			max = b.Count
		}
	}
	c.Empty = c.Total == 0
	c.Max = niceCeil(max)

	plotW := float64(chartWidth - chartPadLeft - chartPadRght)
	plotH := float64(chartHeight - chartPadTop - chartPadBot)
	// A single bucket would divide by zero; pin it to the left edge instead.
	step := plotW
	if len(buckets) > 1 {
		step = plotW / float64(len(buckets)-1)
	}

	noun := func(n int) string {
		if n == 1 {
			return "1 signup"
		}
		return fmt.Sprintf("%d signups", n)
	}

	c.Points = make([]ChartPoint, len(buckets))
	line := make([]string, len(buckets))
	for i, b := range buckets {
		x := float64(chartPadLeft) + step*float64(i)
		y := float64(chartPadTop) + plotH*(1-float64(b.Count)/float64(c.Max))
		label := ""
		// Label the first bucket and every nth after it, and always the last, so
		// the axis is anchored at both ends whatever the interval divides into.
		if i%r.LabelEvery == 0 || i == len(buckets)-1 {
			label = b.Start.Format(r.TimeFormat)
		}
		c.Points[i] = ChartPoint{
			X: x, Y: y, Label: label, Value: b.Count, Bucket: i,
			Title: fmt.Sprintf("%s — %s", b.Start.Format(r.TimeFormat), noun(b.Count)),
		}
		line[i] = fmt.Sprintf("%.2f,%.2f", x, y)
	}
	c.Line = strings.Join(line, " ")

	baseline := float64(chartPadTop) + plotH
	c.Area = fmt.Sprintf("M %.2f,%.2f L %s L %.2f,%.2f Z",
		c.Points[0].X, baseline,
		c.Line,
		c.Points[len(c.Points)-1].X, baseline)

	// Four gridlines including the baseline, at even fractions of the nice max —
	// which is chosen to divide cleanly, so the labels stay whole numbers.
	const gridSteps = 4
	c.Grid = make([]GridLine, gridSteps+1)
	for i := range c.Grid {
		frac := float64(i) / gridSteps
		c.Grid[i] = GridLine{
			Y:     float64(chartPadTop) + plotH*(1-frac),
			Value: int(math.Round(frac * float64(c.Max))),
		}
	}
	return c
}

// niceCeil rounds a max count up to the next multiple of four, so the chart's
// four gridlines always land on whole numbers rather than labels like "3.75".
// Zero maps to 4, giving an empty chart a sane axis instead of a degenerate one.
func niceCeil(n int) int {
	if n <= 4 {
		return 4
	}
	return ((n + 3) / 4) * 4
}

// ChartViewBox is the SVG viewBox for the signups chart.
const ChartViewBox = "0 0 720 200"
