// Package janitor deletes expired auth rows on a schedule.
//
// The store has had PruneExpiredSessions and PruneExpiredTokens since the auth
// tables were created, but nothing ever called them: expired sessions and spent
// verification tokens accumulated for the life of the database. This is the
// caller they were missing.
//
// It is not a correctness fix — every one of those rows is already rejected on
// read, by expiry checks in auth.AuthenticateSession and store.ConsumeToken.
// It is a housekeeping one: unbounded growth in tables that are read on every
// request, holding data about people who are no longer using the app.
package janitor

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sbengtson/budget/internal/core/store"
)

// lockoutIdleFor is how long a lockout row survives after its last failure and
// after any lock has expired. Long enough that a slow attacker cannot reset
// their failure count by pausing, short enough that the table stays small.
const lockoutIdleFor = 24 * time.Hour

type Janitor struct {
	store *store.Store
}

func New(s *store.Store) *Janitor { return &Janitor{store: s} }

// Sweep runs one pass. Separated from Run so tests can drive it directly
// without waiting on a ticker.
//
// Every prune is attempted even when an earlier one fails: they are independent
// tables, and one being briefly unavailable is no reason to skip the others.
func (j *Janitor) Sweep(ctx context.Context) error {
	var errs []error
	if err := j.store.PruneExpiredSessions(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := j.store.PruneExpiredTokens(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := j.store.PruneExpiredLockouts(ctx, lockoutIdleFor); err != nil {
		errs = append(errs, err)
	}
	if err := j.store.PruneExpiredChallenges(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Run sweeps once at startup and then every interval until ctx is cancelled.
//
// The immediate first sweep matters on a box that is restarted often: a purely
// periodic job on a long interval can go a long time without ever running.
func (j *Janitor) Run(ctx context.Context, every time.Duration) {
	sweep := func() {
		if err := j.Sweep(ctx); err != nil {
			slog.Warn("janitor sweep failed", "err", err)
		}
	}
	sweep()

	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
