package ratelimit

import (
	"testing"
	"time"
)

// newTestLimiter returns a Limiter whose clock the test drives by hand, so the
// suite never sleeps.
func newTestLimiter(t *testing.T, r Rule) (*Limiter, func(time.Duration)) {
	t.Helper()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	l := New(r)
	l.now = func() time.Time { return now }
	return l, func(d time.Duration) { now = now.Add(d) }
}

func TestAllowSpendsBurstThenDenies(t *testing.T) {
	l, _ := newTestLimiter(t, Rule{N: 3, Per: time.Minute})

	for i := range 3 {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("event %d should have been allowed", i+1)
		}
	}
	ok, wait := l.Allow("k")
	if ok {
		t.Fatal("4th event should have been denied")
	}
	// 3 per minute refills one token every 20s, and the bucket is empty.
	if wait < 19*time.Second || wait > 21*time.Second {
		t.Fatalf("retry-after = %v, want ~20s", wait)
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	l, advance := newTestLimiter(t, Rule{N: 3, Per: time.Minute})
	for range 3 {
		l.Allow("k")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("bucket should be empty")
	}

	advance(20 * time.Second) // exactly one token
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("one token should have refilled after 20s")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("only one token should have refilled")
	}
}

func TestRefillIsCappedAtBurst(t *testing.T) {
	l, advance := newTestLimiter(t, Rule{N: 2, Per: time.Minute})
	l.Allow("k")
	l.Allow("k")

	advance(24 * time.Hour) // vastly more than enough to refill

	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("first should be allowed")
	}
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("second should be allowed")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("credit must cap at the burst size, not accumulate")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(t, Rule{N: 1, Per: time.Minute})
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a should be allowed")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a should now be exhausted")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b must not be affected by a's spending")
	}
}

// The sweeper is the part a hand-rolled limiter usually gets wrong: without it
// the bucket map grows for every unique IP and address seen for the life of the
// process.
func TestSweepDropsIdleBucketsAndKeepsActiveOnes(t *testing.T) {
	l, advance := newTestLimiter(t, Rule{N: 5, Per: time.Minute})
	l.Allow("idle")
	advance(3 * time.Minute) // > 2*Per, so "idle" has fully refilled
	l.Allow("active")

	if got := l.len(); got != 2 {
		t.Fatalf("buckets = %d, want 2 before sweep", got)
	}
	l.Sweep()
	if got := l.len(); got != 1 {
		t.Fatalf("buckets = %d, want 1 after sweep", got)
	}
	// Dropping a full bucket is not observable: the key still gets its allowance.
	if ok, _ := l.Allow("idle"); !ok {
		t.Fatal("a swept key must still be allowed")
	}
}

func TestNewPanicsOnInvalidRule(t *testing.T) {
	for _, r := range []Rule{{N: 0, Per: time.Minute}, {N: 5, Per: 0}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("New(%+v) should panic", r)
				}
			}()
			New(r)
		}()
	}
}

func TestRegistryAddGetAndDuplicatePanic(t *testing.T) {
	r := NewRegistry()
	l := r.Add("login", Rule{N: 1, Per: time.Minute})
	if r.Get("login") != l {
		t.Fatal("Get should return the added limiter")
	}
	if r.Get("missing") != nil {
		t.Fatal("Get of an unknown name should be nil")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Add should panic")
		}
	}()
	r.Add("login", Rule{N: 1, Per: time.Minute})
}
