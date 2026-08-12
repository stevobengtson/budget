package i18n

import "time"

// Date field names and patterns per locale.
//
// Go's time.Format is English-only by design — the reference layout "Jan 2006"
// has no notion of a locale, and there is no stdlib hook to supply one. So
// anything user-facing has to come from tables like these rather than from a
// layout string.
//
// The tables and patterns were generated from CLDR via Intl.DateTimeFormat
// (ICU 78) and should be regenerated the same way if a locale is added.

// DateFormat holds a locale's month and weekday names plus the field order for
// the handful of date shapes the app renders.
type DateFormat struct {
	MonthsShort   [12]string
	MonthsLong    [12]string
	WeekdaysShort [7]string // indexed by time.Weekday, Sunday first

	// MonthYear renders a month and year, e.g. "Jan 2026".
	MonthYear func(month, year string) string
	// Date renders a full date, e.g. "Jan 2, 2026" or "2 janvier 2026".
	Date func(month, day, year string) string
	// WeekdayDate renders a weekday with a day and month, e.g. "Fri, Jan 2".
	WeekdayDate func(weekday, month, day string) string
}

// Dates returns the locale's date field names and orderings.
//
// Verified against Intl.DateTimeFormat for 2 January 2026:
//
//	en-CA   "Jan 2026"    "Jan 2, 2026"   "January 2, 2026"   "Fri, Jan 2"
//	fr-CA   "janv. 2026"  "2 janv. 2026"  "2 janvier 2026"    "ven. 2 janv."
//
// French is not merely reordered English: the day precedes the month, there is
// no comma before the year, and month names are lowercase.
func (l Locale) Dates() DateFormat {
	switch l {
	case FrCA:
		return DateFormat{
			MonthsShort: [12]string{
				"janv.", "févr.", "mars", "avr.", "mai", "juin",
				"juill.", "août", "sept.", "oct.", "nov.", "déc.",
			},
			MonthsLong: [12]string{
				"janvier", "février", "mars", "avril", "mai", "juin",
				"juillet", "août", "septembre", "octobre", "novembre", "décembre",
			},
			WeekdaysShort: [7]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."},

			MonthYear:   func(month, year string) string { return month + " " + year },
			Date:        func(month, day, year string) string { return day + " " + month + " " + year },
			WeekdayDate: func(weekday, month, day string) string { return weekday + " " + day + " " + month },
		}
	default:
		return DateFormat{
			MonthsShort: [12]string{
				"Jan", "Feb", "Mar", "Apr", "May", "Jun",
				"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
			},
			MonthsLong: [12]string{
				"January", "February", "March", "April", "May", "June",
				"July", "August", "September", "October", "November", "December",
			},
			WeekdaysShort: [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},

			MonthYear:   func(month, year string) string { return month + " " + year },
			Date:        func(month, day, year string) string { return month + " " + day + ", " + year },
			WeekdayDate: func(weekday, month, day string) string { return weekday + ", " + month + " " + day },
		}
	}
}

// MonthShort returns the abbreviated month name for t in this locale.
func (l Locale) MonthShort(t time.Time) string {
	return l.Dates().MonthsShort[int(t.Month())-1]
}

// MonthLong returns the full month name for t in this locale.
func (l Locale) MonthLong(t time.Time) string {
	return l.Dates().MonthsLong[int(t.Month())-1]
}

// WeekdayShort returns the abbreviated weekday name for t in this locale.
func (l Locale) WeekdayShort(t time.Time) string {
	return l.Dates().WeekdaysShort[int(t.Weekday())]
}
