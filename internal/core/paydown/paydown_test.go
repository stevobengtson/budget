package paydown

import (
	"testing"
	"time"
)

func TestVisaSpreadsheetShape(t *testing.T) {
	// User's spreadsheet: Visa APR 20.99%, start $42,856.59 on Jan 19, 2026
	// statement, paying $800/mo. Apr 2026 row: start $41,991.83, interest
	// $755.09, payment $800.00, end $42,469.88 (approx, with rounding drift
	// because the spreadsheet uses statement-day cycles).
	//
	// We project from a calendar month standpoint, so values won't match
	// exactly — we just confirm shape: starts amortizing, interest is
	// proportional to balance, and the loan eventually pays off.
	apr := int64(2099) // 20.99% in bps
	start := int64(42_856_59)
	pay := int64(80_000)

	p, err := Compute(1, "Visa", apr, start, FlatSchedule(pay, 360), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) == 0 {
		t.Fatal("expected projected rows")
	}
	first := p.Rows[0]
	if first.InterestCents <= 0 {
		t.Errorf("first month interest should be > 0, got %d", first.InterestCents)
	}
	if first.PaymentCents != pay {
		t.Errorf("first payment = %d, want %d", first.PaymentCents, pay)
	}
	if first.BalanceCents >= start {
		t.Errorf("balance should drop after $800 payment; got %d ≥ start %d",
			first.BalanceCents, start)
	}
	if p.PayoffMonth.IsZero() {
		t.Error("loan should pay off within 360 months at $800/mo")
	}
	if p.Diverging {
		t.Error("$800/mo should be > monthly interest, not diverging")
	}
}

func TestZeroBalanceShortCircuits(t *testing.T) {
	p, err := Compute(1, "X", 1000, 0, FlatSchedule(10_000, 12), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 0 {
		t.Errorf("expected no rows for zero balance, got %d", len(p.Rows))
	}
}

func TestDivergingDetected(t *testing.T) {
	// 30% APR on $10,000 → daily 30%/365 ≈ 0.0822% → ~ $254/mo interest.
	// Paying $50/mo is below interest, debt should grow.
	p, err := Compute(1, "Bad", 3000, 1_000_000, FlatSchedule(5_000, 6), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Diverging {
		t.Error("should detect diverging loan")
	}
	if last := p.Rows[len(p.Rows)-1].BalanceCents; last <= p.StartCents {
		t.Errorf("balance should grow when underpaying, got %d → %d", p.StartCents, last)
	}
}

func TestVariableSchedule(t *testing.T) {
	// $10,000 debt, 12% APR, alternating $500 / $1,000 payments.
	// Verify each row's PaymentCents matches the schedule and Source carries.
	schedule := []MonthPayment{
		{Cents: 50_000, Source: SourceSpent},
		{Cents: 100_000, Source: SourceAssigned},
		{Cents: 50_000, Source: SourceDefault},
	}
	p, err := Compute(1, "V", 1200, 1_000_000, schedule, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 3 {
		t.Fatalf("len rows = %d, want 3", len(p.Rows))
	}
	for i, want := range []struct {
		amt int64
		src PaymentSource
	}{
		{50_000, SourceSpent},
		{100_000, SourceAssigned},
		{50_000, SourceDefault},
	} {
		if p.Rows[i].PaymentCents != want.amt {
			t.Errorf("row %d payment = %d, want %d", i, p.Rows[i].PaymentCents, want.amt)
		}
		if p.Rows[i].PaymentSource != want.src {
			t.Errorf("row %d source = %v, want %v", i, p.Rows[i].PaymentSource, want.src)
		}
	}
}

func TestDaysInMonth(t *testing.T) {
	cases := []struct {
		t    time.Time
		want int
	}{
		{time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 31},
		{time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 28},
		{time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), 29},
		{time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), 30},
	}
	for _, c := range cases {
		if got := daysInMonth(c.t); got != c.want {
			t.Errorf("daysInMonth(%s) = %d, want %d", c.t.Format("2006-01"), got, c.want)
		}
	}
}
