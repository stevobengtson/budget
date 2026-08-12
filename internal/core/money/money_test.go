package money

import (
	"testing"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// nb is the NO-BREAK SPACE fr-CA groups and spaces with. Spelled out here so
// the expectations below are readable — a literal U+00A0 in a test string looks
// exactly like a plain space, which is the bug it would be hiding.
const nb = "\u00a0"

func TestParseEnCA(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"0", 0, false},
		{"1", 100, false},
		{"1.5", 150, false},
		{"1.50", 150, false},
		{"1.05", 105, false},
		{"1234.56", 123456, false},
		{"$1,234.56", 123456, false},
		{" -50 ", -5000, false},
		{"-0.99", -99, false},
		{".5", 50, false},
		{"+10", 1000, false},
		{"1,500", 150000, false}, // comma groups in en-CA
		{"", 0, true},
		{"abc", 0, true},
		{"1.234", 0, true},
		{"1.2.3", 0, true},
	}
	for _, c := range cases {
		got, err := ParseIn(i18n.EnCA, c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseIn(en-CA, %q) expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseIn(en-CA, %q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseIn(en-CA, %q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseFrCA(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"0", 0, false},
		{"1", 100, false},
		{"1,5", 150, false},
		{"1,50", 150, false},
		{"1234,56", 123456, false},
		{"1 234,56 $", 123456, false},                   // typed with a plain space
		{"1" + nb + "234,56" + nb + "$", 123456, false}, // pasted back from our own output
		{"1 234,56", 123456, false},
		{" -50 ", -5000, false},
		{",5", 50, false},
		{"", 0, true},
		// A comma is the decimal point in fr-CA, so "1,500" is three fractional
		// digits and is rejected rather than silently read as one thousand five
		// hundred. See decimalSep.
		{"1,500", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := ParseIn(i18n.FrCA, c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseIn(fr-CA, %q) expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseIn(fr-CA, %q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseIn(fr-CA, %q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// The expected strings here were produced by Intl.NumberFormat under ICU 78,
// not by reading this package's own code, so a change to the formatter that
// drifts away from CLDR fails here.
func TestFormat(t *testing.T) {
	cases := []struct {
		locale i18n.Locale
		in     int64
		want   string
	}{
		{i18n.EnCA, 0, "$0.00"},
		{i18n.EnCA, 5, "$0.05"},
		{i18n.EnCA, 50, "$0.50"},
		{i18n.EnCA, 100, "$1.00"},
		{i18n.EnCA, 12345, "$123.45"},
		{i18n.EnCA, 1234567, "$12,345.67"},
		{i18n.EnCA, -5000, "-$50.00"},
		{i18n.EnCA, -1234567, "-$12,345.67"},
		{i18n.EnCA, 1000000000, "$10,000,000.00"},
		{i18n.EnCA, 123456789, "$1,234,567.89"},

		{i18n.FrCA, 0, "0,00" + nb + "$"},
		{i18n.FrCA, 5, "0,05" + nb + "$"},
		{i18n.FrCA, 100, "1,00" + nb + "$"},
		{i18n.FrCA, 12345, "123,45" + nb + "$"},
		{i18n.FrCA, 1234567, "12" + nb + "345,67" + nb + "$"},
		{i18n.FrCA, -5000, "-50,00" + nb + "$"},
		{i18n.FrCA, -1234567, "-12" + nb + "345,67" + nb + "$"},
		{i18n.FrCA, 123456789, "1" + nb + "234" + nb + "567,89" + nb + "$"},
	}
	for _, c := range cases {
		if got := FormatIn(c.locale, c.in); got != c.want {
			t.Errorf("FormatIn(%s, %d) = %q, want %q", c.locale, c.in, got, c.want)
		}
	}
}

// Whatever we render, we must be able to read back — a user copying an amount
// out of the app and pasting it into an input is ordinary behaviour, and in
// fr-CA the rendered form is full of non-breaking spaces.
func TestRoundTrip(t *testing.T) {
	for _, l := range i18n.Supported {
		for _, v := range []int64{0, 1, 99, 100, 12345, -67, -98765, 123456789} {
			s := FormatIn(l, v)
			got, err := ParseIn(l, s)
			if err != nil {
				t.Errorf("ParseIn(%s, %q): %v", l, s, err)
				continue
			}
			if got != v {
				t.Errorf("%s round-trip %d -> %q -> %d", l, v, s, got)
			}
		}
	}
}
