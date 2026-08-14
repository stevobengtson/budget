package store

import (
	"context"
	"testing"
	"time"
)

func TestGetLockoutForUnknownSubjectIsZeroNotError(t *testing.T) {
	s := newTestStore(t)
	l, err := s.GetLockout(context.Background(), ScopePasswordLogin, "nobody@example.com")
	if err != nil {
		t.Fatalf("unknown subject should not error: %v", err)
	}
	if l.Failures != 0 || l.Locked(time.Now()) {
		t.Fatalf("want zero lockout, got %+v", l)
	}
}

func TestRecordLoginFailureAccumulates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const subject = "victim@example.com"

	for want := 1; want <= 3; want++ {
		got, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, subject, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("failures = %d, want %d", got, want)
		}
	}

	l, err := s.GetLockout(ctx, ScopePasswordLogin, subject)
	if err != nil {
		t.Fatal(err)
	}
	if l.Failures != 3 {
		t.Fatalf("stored failures = %d, want 3", l.Failures)
	}
	if l.Locked(time.Now()) {
		t.Fatal("recording failures must not lock on its own — that is the service's decision")
	}
}

// A stale counter must not accumulate across a long gap, or a handful of typos
// spread over months would eventually lock an innocent account.
func TestRecordLoginFailureResetsAfterWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const subject = "stale@example.com"

	if _, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, subject, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, subject, time.Hour); err != nil {
		t.Fatal(err)
	}
	// A zero window makes every prior failure instantly "old".
	got, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, subject, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("failures = %d, want the counter to reset to 1", got)
	}
}

func TestLockAndClearSubject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const subject = "locked@example.com"

	if _, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, subject, time.Hour); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(15 * time.Minute)
	if err := s.LockSubject(ctx, ScopePasswordLogin, subject, until); err != nil {
		t.Fatal(err)
	}

	l, err := s.GetLockout(ctx, ScopePasswordLogin, subject)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Locked(time.Now()) {
		t.Fatal("subject should be locked")
	}
	if l.Locked(until.Add(time.Second)) {
		t.Fatal("lock must expire at locked_until")
	}

	if err := s.ClearLoginFailures(ctx, ScopePasswordLogin, subject); err != nil {
		t.Fatal(err)
	}
	l, err = s.GetLockout(ctx, ScopePasswordLogin, subject)
	if err != nil {
		t.Fatal(err)
	}
	if l.Failures != 0 || l.Locked(time.Now()) {
		t.Fatalf("a successful sign-in should clear everything, got %+v", l)
	}
}

func TestPruneExpiredLockoutsKeepsLiveLocks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, "idle@example.com", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordLoginFailure(ctx, ScopePasswordLogin, "held@example.com", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.LockSubject(ctx, ScopePasswordLogin, "held@example.com", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// A zero idle window makes both rows "old"; only the live lock survives.
	if err := s.PruneExpiredLockouts(ctx, 0); err != nil {
		t.Fatal(err)
	}

	idle, err := s.GetLockout(ctx, ScopePasswordLogin, "idle@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if idle.Failures != 0 {
		t.Fatal("idle unlocked row should have been pruned")
	}
	held, err := s.GetLockout(ctx, ScopePasswordLogin, "held@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !held.Locked(time.Now()) {
		t.Fatal("pruning must never release a lock that is still in force")
	}
}
