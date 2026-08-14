package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LockoutScope names a family of attempts that lock independently. Only
// password login has one today; passkey and OAuth sign-ins are deliberately not
// lockable, so that an attacker cannot deny a victim every way in at once by
// spraying the one factor they do not need.
type LockoutScope string

const ScopePasswordLogin LockoutScope = "password_login"

// Lockout is the failure state for one subject (a normalized email) in one
// scope. A zero LockedUntil means "counting failures, not yet locked".
type Lockout struct {
	Subject       string
	Scope         LockoutScope
	Failures      int
	LockedUntil   time.Time
	LastFailureAt time.Time
}

// Locked reports whether the lock is currently in force.
func (l Lockout) Locked(now time.Time) bool {
	return !l.LockedUntil.IsZero() && now.Before(l.LockedUntil)
}

// GetLockout returns the current failure state for subject. A subject that has
// never failed yields a zero Lockout and a nil error — "no record" is a normal
// answer here, not an error worth propagating to every caller.
func (s *Store) GetLockout(ctx context.Context, scope LockoutScope, subject string) (Lockout, error) {
	l := Lockout{Scope: scope, Subject: subject}
	var lockedUntil sql.NullTime
	err := s.queryOne(ctx,
		`SELECT failures, locked_until, last_failure_at
		 FROM auth_lockouts WHERE scope = $1 AND subject = $2`,
		string(scope), subject).
		Scan(&l.Failures, &lockedUntil, &l.LastFailureAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Lockout{Scope: scope, Subject: subject}, nil
	}
	if err != nil {
		return Lockout{}, fmt.Errorf("get lockout: %w", err)
	}
	if lockedUntil.Valid {
		l.LockedUntil = lockedUntil.Time
	}
	return l, nil
}

// RecordLoginFailure increments the failure counter for subject and returns the
// new total. It deliberately does not decide whether that total warrants a lock:
// the escalation policy lives in the auth service, so the store stays a record
// of what happened rather than a holder of security policy.
//
// The counter resets if the last failure is older than window — otherwise a
// single stray typo months ago would count toward today's lockout, and the
// threshold would slowly become unreachable-by-accident for long-lived accounts.
func (s *Store) RecordLoginFailure(ctx context.Context, scope LockoutScope, subject string, window time.Duration) (int, error) {
	var failures int
	err := s.queryOne(ctx,
		`INSERT INTO auth_lockouts (scope, subject, failures, last_failure_at, updated_at)
		 VALUES ($1, $2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT (scope, subject) DO UPDATE SET
		     failures = CASE
		         WHEN auth_lockouts.last_failure_at < CURRENT_TIMESTAMP - $3::interval THEN 1
		         ELSE auth_lockouts.failures + 1
		     END,
		     last_failure_at = CURRENT_TIMESTAMP,
		     updated_at      = CURRENT_TIMESTAMP
		 RETURNING failures`,
		string(scope), subject, window.String()).Scan(&failures)
	if err != nil {
		return 0, fmt.Errorf("record login failure: %w", err)
	}
	return failures, nil
}

// LockSubject sets the lock expiry. Called by the auth service once its policy
// decides the accumulated failures warrant one.
func (s *Store) LockSubject(ctx context.Context, scope LockoutScope, subject string, until time.Time) error {
	_, err := s.run(ctx,
		`UPDATE auth_lockouts SET locked_until = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE scope = $1 AND subject = $2`,
		string(scope), subject, until)
	if err != nil {
		return fmt.Errorf("lock subject: %w", err)
	}
	return nil
}

// ClearLoginFailures drops the record entirely after a successful sign-in.
func (s *Store) ClearLoginFailures(ctx context.Context, scope LockoutScope, subject string) error {
	_, err := s.run(ctx,
		`DELETE FROM auth_lockouts WHERE scope = $1 AND subject = $2`,
		string(scope), subject)
	if err != nil {
		return fmt.Errorf("clear login failures: %w", err)
	}
	return nil
}

// PruneExpiredLockouts removes rows that are neither locked nor recently active.
// Run by the janitor.
func (s *Store) PruneExpiredLockouts(ctx context.Context, idleFor time.Duration) error {
	_, err := s.run(ctx,
		`DELETE FROM auth_lockouts
		 WHERE (locked_until IS NULL OR locked_until < CURRENT_TIMESTAMP)
		   AND last_failure_at < CURRENT_TIMESTAMP - $1::interval`,
		idleFor.String())
	return err
}
