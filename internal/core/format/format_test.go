package format

import (
	"context"
	"testing"
	"time"

	"github.com/sbengtson/budget/internal/core/i18n"
)

func cents(c int64) *int64 { return &c }

func en() context.Context { return i18n.WithLocale(context.Background(), i18n.EnCA) }
func fr() context.Context { return i18n.WithLocale(context.Background(), i18n.FrCA) }

func TestGoalForNoGoal(t *testing.T) {
	if _, ok := GoalFor(en(), nil, nil, 0); ok {
		t.Fatal("expected ok=false when goalCents is nil")
	}
}

func TestGoalForAmountOnly(t *testing.T) {
	g, ok := GoalFor(en(), cents(185000), nil, 0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if g.Amount != "$1,850.00" {
		t.Fatalf("Amount = %q, want $1,850.00", g.Amount)
	}
	if g.Due != "" || g.Need != "" {
		t.Fatalf("Due/Need should be empty, got %q / %q", g.Due, g.Need)
	}
}

func TestGoalForFull(t *testing.T) {
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	g, ok := GoalFor(en(), cents(300000), &due, 15000)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if g.Due != "Sep 2026" {
		t.Fatalf("Due = %q, want Sep 2026", g.Due)
	}
	if g.Need != "$150.00/mo" {
		t.Fatalf("Need = %q, want $150.00/mo", g.Need)
	}
}

// The French goal line differs in all three parts at once — amount format, month
// name, and the "/mo" suffix — which is the whole reason GoalFor takes a context
// rather than being a pure string builder.
func TestGoalForFrench(t *testing.T) {
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	g, ok := GoalFor(fr(), cents(300000), &due, 15000)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "3" + nb + "000,00" + nb + "$"; g.Amount != want {
		t.Errorf("Amount = %q, want %q", g.Amount, want)
	}
	if g.Due != "sept. 2026" {
		t.Errorf("Due = %q, want sept. 2026", g.Due)
	}
	if want := "150,00" + nb + "$/mois"; g.Need != want {
		t.Errorf("Need = %q, want %q", g.Need, want)
	}
}

// nb is the NO-BREAK SPACE fr-CA groups with; see the money package.
const nb = "\u00a0"

// The date shapes were taken from Intl.DateTimeFormat (ICU 78) for 2 Jan 2026.
// French is not reordered English: day precedes month, there is no comma before
// the year, and month names are lowercase.
func TestDates(t *testing.T) {
	d := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) // a Friday
	cases := []struct {
		name string
		fn   func(context.Context, time.Time) string
		en   string
		fr   string
	}{
		{"MonthYear", MonthYear, "Jan 2026", "janv. 2026"},
		{"MonthYearLong", MonthYearLong, "January 2026", "janvier 2026"},
		{"DateMedium", DateMedium, "Jan 2, 2026", "2 janv. 2026"},
		{"DateLong", DateLong, "January 2, 2026", "2 janvier 2026"},
		{"WeekdayDate", WeekdayDate, "Fri, Jan 2", "ven. 2 janv."},
	}
	for _, c := range cases {
		if got := c.fn(en(), d); got != c.en {
			t.Errorf("%s(en-CA) = %q, want %q", c.name, got, c.en)
		}
		if got := c.fn(fr(), d); got != c.fr {
			t.Errorf("%s(fr-CA) = %q, want %q", c.name, got, c.fr)
		}
	}
}

// Every month and weekday name is used somewhere, and a typo in one cell of the
// table would otherwise only surface in that month.
func TestAllMonthAndWeekdayNames(t *testing.T) {
	wantFrShort := []string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juill.", "août", "sept.", "oct.", "nov.", "déc."}
	wantFrLong := []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	for m := 0; m < 12; m++ {
		d := time.Date(2026, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC)
		if got := i18n.FrCA.MonthShort(d); got != wantFrShort[m] {
			t.Errorf("fr MonthShort(%d) = %q, want %q", m+1, got, wantFrShort[m])
		}
		if got := i18n.FrCA.MonthLong(d); got != wantFrLong[m] {
			t.Errorf("fr MonthLong(%d) = %q, want %q", m+1, got, wantFrLong[m])
		}
	}

	wantFrDays := []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	// 2026-02-01 is a Sunday.
	for i := 0; i < 7; i++ {
		d := time.Date(2026, 2, 1+i, 0, 0, 0, 0, time.UTC)
		if got := i18n.FrCA.WeekdayShort(d); got != wantFrDays[i] {
			t.Errorf("fr WeekdayShort(%s) = %q, want %q", d.Weekday(), got, wantFrDays[i])
		}
	}
}
