package format

import (
	"testing"
	"time"
)

func cents(c int64) *int64 { return &c }

func TestGoalForNoGoal(t *testing.T) {
	if _, ok := GoalFor(nil, nil, 0); ok {
		t.Fatal("expected ok=false when goalCents is nil")
	}
}

func TestGoalForAmountOnly(t *testing.T) {
	g, ok := GoalFor(cents(185000), nil, 0)
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
	g, ok := GoalFor(cents(300000), &due, 15000)
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
