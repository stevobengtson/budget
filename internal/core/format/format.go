// Package format holds presentation helpers shared by the tui and web
// clients so wording (goal summaries, date labels) stays consistent.
//
// Everything here is locale-dependent and takes a context, which Templ hands
// every component as `ctx`. Go's time.Format cannot help with any of it: the
// reference layout "Jan 2006" is English by construction and the stdlib offers
// no way to supply month names, so localized dates are assembled from the
// tables in the i18n package instead of from a layout string.
package format

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/money"
)

// MonthYear renders an abbreviated month and year: "Jan 2026" / "janv. 2026".
func MonthYear(ctx context.Context, t time.Time) string {
	l := i18n.LocaleFrom(ctx)
	return l.Dates().MonthYear(l.MonthShort(t), strconv.Itoa(t.Year()))
}

// MonthYearLong renders a full month and year: "January 2026" / "janvier 2026".
func MonthYearLong(ctx context.Context, t time.Time) string {
	l := i18n.LocaleFrom(ctx)
	return l.Dates().MonthYear(l.MonthLong(t), strconv.Itoa(t.Year()))
}

// DateMedium renders an abbreviated date: "Jan 2, 2026" / "2 janv. 2026".
func DateMedium(ctx context.Context, t time.Time) string {
	l := i18n.LocaleFrom(ctx)
	return l.Dates().Date(l.MonthShort(t), strconv.Itoa(t.Day()), strconv.Itoa(t.Year()))
}

// DateLong renders a full date: "January 2, 2026" / "2 janvier 2026".
func DateLong(ctx context.Context, t time.Time) string {
	l := i18n.LocaleFrom(ctx)
	return l.Dates().Date(l.MonthLong(t), strconv.Itoa(t.Day()), strconv.Itoa(t.Year()))
}

// WeekdayDate renders a weekday with its day and month, for the transaction
// day headings: "Fri, Jan 2" / "ven. 2 janv.".
func WeekdayDate(ctx context.Context, t time.Time) string {
	l := i18n.LocaleFrom(ctx)
	return l.Dates().WeekdayDate(l.WeekdayShort(t), l.MonthShort(t), strconv.Itoa(t.Day()))
}

// Decimal renders a plain number with the locale's decimal separator, for
// figures that are not money — an interest rate, a percentage. en-CA writes
// 19.99, fr-CA writes 19,99.
//
// No thousands grouping: the values this is for (rates, percentages) do not
// reach four digits, and grouping them would read as an error.
func Decimal(ctx context.Context, v float64, places int) string {
	s := strconv.FormatFloat(v, 'f', places, 64)
	if dec := i18n.LocaleFrom(ctx).Numbers().Decimal; dec != "." {
		s = strings.Replace(s, ".", dec, 1)
	}
	return s
}

// ParseDecimal reads a plain number typed in any supported locale, accepting
// either separator.
//
// Deliberately more lenient than money.Parse, and safely so: money has to
// decide whether a comma groups thousands or marks the decimal, and guessing
// wrong there is an error of 1000x. A rate has no grouping — "19,99" and
// "19.99" can only mean the same number — so accepting both costs nothing and
// means a French user typing a comma is not silently ignored.
func ParseDecimal(ctx context.Context, s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

// Goal holds the formatted pieces of a category goal. Callers assemble them
// however their medium requires (plain text for the tui, HTML spans for web).
type Goal struct {
	Amount string // formatted goal amount, e.g. "$1,850.00"
	Due    string // formatted due date ("Jan 2026"), empty when none
	Need   string // formatted monthly need, e.g. "$150.00/mo", empty when target <= 0
}

// GoalFor builds the formatted goal pieces. ok is false when no goal is set.
func GoalFor(ctx context.Context, goalCents *int64, due *time.Time, monthlyTarget int64) (g Goal, ok bool) {
	if goalCents == nil {
		return Goal{}, false
	}
	g.Amount = money.Format(ctx, *goalCents)
	if due != nil {
		g.Due = MonthYear(ctx, *due)
	}
	if monthlyTarget > 0 {
		// "/mo" is a word, not punctuation — French wants "/mois" — so it comes
		// from the catalog rather than being concatenated here.
		g.Need = money.Format(ctx, monthlyTarget) + i18n.T(ctx, "budget.per_month_suffix")
	}
	return g, true
}
