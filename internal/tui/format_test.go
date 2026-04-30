package tui

import "testing"

func TestFormatMonth(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-04", "Apr 2026"},
		{"2024-12", "Dec 2024"},
		{"bad", "bad"},
	}
	for _, c := range cases {
		if got := formatMonth(c.in); got != c.want {
			t.Errorf("formatMonth(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
