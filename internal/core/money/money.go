// Package money handles cents <-> human string conversion.
// All amounts are non-negative cents; sign is implied by outflow/inflow.
package money

import (
	"errors"
	"fmt"
	"strings"
)

// Parse accepts strings like "1234.56", "$1,234.56", "1234", "-50", ".5".
// Returns signed cents.
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(s)
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
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, errors.New("missing digits")
	}

	dot := strings.IndexByte(s, '.')
	var dollars, cents string
	if dot < 0 {
		dollars = s
		cents = "00"
	} else {
		dollars = s[:dot]
		cents = s[dot+1:]
		if dollars == "" {
			dollars = "0"
		}
		if len(cents) == 0 {
			cents = "00"
		} else if len(cents) == 1 {
			cents = cents + "0"
		} else if len(cents) > 2 {
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

// Format renders signed cents as "$1,234.56" / "-$50.00".
func Format(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	dollars := cents / 100
	frac := cents % 100

	// Group thousands.
	ds := fmt.Sprintf("%d", dollars)
	if len(ds) > 3 {
		var b strings.Builder
		first := len(ds) % 3
		if first > 0 {
			b.WriteString(ds[:first])
			if len(ds) > first {
				b.WriteByte(',')
			}
		}
		for i := first; i < len(ds); i += 3 {
			b.WriteString(ds[i : i+3])
			if i+3 < len(ds) {
				b.WriteByte(',')
			}
		}
		ds = b.String()
	}

	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s$%s.%02d", sign, ds, frac)
}
