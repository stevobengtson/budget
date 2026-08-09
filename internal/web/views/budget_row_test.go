package views

import "testing"

// The progress bar measures spending against the whole envelope — carryover
// plus this month's assignment — which the store expresses as
// Available = carryover + assigned − spent, i.e. envelope = spent + available.
// Measuring against the assignment alone understates any category carrying
// money forward.
func TestProgressPct(t *testing.T) {
	cases := []struct {
		name      string
		spent     int64
		available int64
		wantPct   int
		wantOver  bool
	}{
		// The reported bug: $60 rolled over, $5 assigned, $1 spent. The envelope
		// is $65, so this is 1% used — not the 20% that "of $5.00" implied.
		{"rollover, barely spent", 100, 6400, 1, false},
		// Same category once spending passes the envelope.
		{"rollover, overspent", 9500, -3000, 100, true},
		// No rollover: envelope is just the assignment.
		{"plain half spent", 5000, 5000, 50, false},
		{"plain fully spent", 10000, 0, 100, false},
		{"plain overspent", 12000, -2000, 100, true},
		// Empty category: nothing assigned, nothing spent.
		{"untouched", 0, 0, 0, false},
		// Spending with nothing in the envelope is overspending.
		{"spent with nothing assigned", 2500, -2500, 100, true},
		// A refund can push spent negative; the bar must not go negative with it.
		{"net refund", -1500, 6500, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pct, over := progressPct(c.spent, c.available)
			if pct != c.wantPct || over != c.wantOver {
				t.Errorf("progressPct(%d, %d) = (%d, %v), want (%d, %v)",
					c.spent, c.available, pct, over, c.wantPct, c.wantOver)
			}
			if pct < 0 || pct > 100 {
				t.Errorf("pct %d outside 0-100", pct)
			}
		})
	}
}

// The bar's state and the Available column are driven by the same figure, so
// they cannot contradict each other — which is what made the original bug
// confusing to read.
func TestProgressOverspentMatchesAvailable(t *testing.T) {
	for _, available := range []int64{-1, -3000, 0, 1, 6400} {
		_, over := progressPct(1000, available)
		if want := available < 0; over != want {
			t.Errorf("available %d: overspent = %v, want %v", available, over, want)
		}
	}
}
