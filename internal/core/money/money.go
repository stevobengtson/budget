// Package money handles cents <-> human string conversion.
// All amounts are non-negative cents; sign is implied by outflow/inflow.
//
// Both directions are locale-dependent. Canadian English writes $1,234.56;
// Canadian French writes 1 234,56 $ — the separators swap roles and the symbol
// moves. Formatting takes the locale from the request context, which Templ
// hands every component as `ctx`, matching how i18n.T is called.
package money

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// Parse reads a user-entered amount in the context's locale and returns signed
// cents. See ParseIn for the accepted forms.
func Parse(ctx context.Context, s string) (int64, error) {
	return ParseIn(i18n.LocaleFrom(ctx), s)
}

// ParseIn reads a user-entered amount in an explicit locale.
//
// Accepted, in either locale: an optional leading sign, an optional currency
// symbol on either side, group separators (comma, plain space, or U+00A0), and
// at most two fractional digits after the locale's decimal separator.
//
//	en-CA   "1234.56"  "$1,234.56"  "1234"  "-50"  ".5"
//	fr-CA   "1234,56"  "1 234,56 $" "1234"  "-50"  ",5"
//
// The separators are read strictly per locale: in fr-CA a comma is always the
// decimal point and never a group separator, and in en-CA the reverse. See
// decimalSep for why.
func ParseIn(l i18n.Locale, s string) (int64, error) {
	nf := l.Numbers()

	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\u00a0", " ") // NBSP we emitted on the way out
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), nf.Symbol))
	if s == "" {
		return 0, errors.New("empty amount")
	}

	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), nf.Symbol)
	s = strings.TrimSpace(s)

	dec := decimalSep(l)
	// Strip group separators. Anything that is not the decimal separator is one:
	// a comma in fr-CA is the decimal point, so it is not stripped there, and a
	// space is never a decimal point in either locale.
	for _, g := range []string{",", " "} {
		if g != dec {
			s = strings.ReplaceAll(s, g, "")
		}
	}
	if s == "" {
		return 0, errors.New("missing digits")
	}

	dot := strings.Index(s, dec)
	var dollars, cents string
	if dot < 0 {
		dollars = s
		cents = "00"
	} else {
		dollars = s[:dot]
		cents = s[dot+len(dec):]
		if dollars == "" {
			dollars = "0"
		}
		switch {
		case len(cents) == 0:
			cents = "00"
		case len(cents) == 1:
			cents = cents + "0"
		case len(cents) > 2:
			return 0, fmt.Errorf("too many fractional digits: %q", s)
		}
	}

	var d, c int64
	for _, r := range dollars {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid digit %q", r)
		}
		d = d*10 + int64(r-'0')
	}
	for _, r := range cents {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid digit %q", r)
		}
		c = c*10 + int64(r-'0')
	}

	total := d*100 + c
	if neg {
		total = -total
	}
	return total, nil
}

// decimalSep returns the character that separates dollars from cents when
// parsing input in a locale.
//
// This is the one genuinely ambiguous point in the whole locale story, and it
// is decided here rather than inline so the choice is visible.
//
// The problem input is "1,500". In en-CA that is one thousand five hundred
// dollars. In fr-CA a comma is the decimal point, so it reads as 1.500 dollars
// — three fractional digits, which this parser rejects as an error.
//
// Strict per-locale reading is what happens today: whichever locale the user is
// in, the comma means exactly one thing, and an input that does not fit is
// rejected loudly rather than silently becoming an amount off by 1000x. A
// lenient reader that guessed from digit-count would accept more typing but
// would, on exactly this input, sometimes guess wrong and save the wrong number
// into someone's budget.
func decimalSep(l i18n.Locale) string {
	return l.Numbers().Decimal
}

// Format renders signed cents in the context's locale.
func Format(ctx context.Context, cents int64) string {
	return FormatIn(i18n.LocaleFrom(ctx), cents)
}

// FormatIn renders signed cents in an explicit locale.
//
//	en-CA   "$1,234.56"   "-$50.00"
//	fr-CA   "1 234,56 $"  "-50,00 $"    (separators are U+00A0)
func FormatIn(l i18n.Locale, cents int64) string {
	nf := l.Numbers()

	neg := cents < 0
	if neg {
		cents = -cents
	}
	ds := group(fmt.Sprintf("%d", cents/100), nf.Group)
	amount := ds + nf.Decimal + fmt.Sprintf("%02d", cents%100)

	var b strings.Builder
	if neg {
		b.WriteString("-")
	}
	if nf.SymbolAfter {
		b.WriteString(amount)
		b.WriteString(nf.SymbolSpace)
		b.WriteString(nf.Symbol)
	} else {
		b.WriteString(nf.Symbol)
		b.WriteString(amount)
	}
	return b.String()
}

// group inserts sep between groups of three digits, right to left.
func group(ds string, sep string) string {
	if len(ds) <= 3 {
		return ds
	}
	var b strings.Builder
	first := len(ds) % 3
	if first > 0 {
		b.WriteString(ds[:first])
		b.WriteString(sep)
	}
	for i := first; i < len(ds); i += 3 {
		b.WriteString(ds[i : i+3])
		if i+3 < len(ds) {
			b.WriteString(sep)
		}
	}
	return b.String()
}
