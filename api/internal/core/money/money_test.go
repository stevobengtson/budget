package money

import "testing"

func TestParse(t *testing.T) {
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
		{"", 0, true},
		{"abc", 0, true},
		{"1.234", 0, true},
		{"1.2.3", 0, true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.err {
			if err == nil {
				t.Errorf("Parse(%q) expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "$0.00"},
		{5, "$0.05"},
		{50, "$0.50"},
		{100, "$1.00"},
		{12345, "$123.45"},
		{1234567, "$12,345.67"},
		{-5000, "-$50.00"},
		{-1234567, "-$12,345.67"},
		{1000000000, "$10,000,000.00"},
	}
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("Format(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, 99, 100, 12345, -67, -98765} {
		s := Format(v)
		got, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q): %v", s, err)
			continue
		}
		if got != v {
			t.Errorf("round-trip %d -> %q -> %d", v, s, got)
		}
	}
}
