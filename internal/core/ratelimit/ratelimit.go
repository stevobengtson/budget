// Package ratelimit provides an in-process token-bucket limiter for throttling
// abusive request rates.
//
// In-process, not Postgres-backed, because this app runs as a single binary on
// a single host: there is nothing to coordinate with, and a rejection is the
// path that most needs to be cheap. Backing it with a counter table would mean
// a read and a write to the database before the expensive work (argon2id) is
// even reached, so a spray attack would turn every rejection into two round
// trips — the opposite of what a limiter is for.
//
// The trade-off is that buckets reset when the process restarts. That is
// acceptable for *rate* limiting, whose job is to blunt a burst happening right
// now. It is not acceptable for account lockout, which must survive a deploy;
// that lives in the auth_lockouts table instead.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Rule describes an allowance: N events per Per, with a bucket that holds up to
// Burst events' worth of credit. A zero Burst means N, which is the usual
// "10 per 5 minutes, and you may spend all 10 at once" reading.
type Rule struct {
	N     int
	Per   time.Duration
	Burst int
}

func (r Rule) burst() float64 {
	if r.Burst > 0 {
		return float64(r.Burst)
	}
	return float64(r.N)
}

// ratePerSecond is how quickly credit is restored.
func (r Rule) ratePerSecond() float64 {
	return float64(r.N) / r.Per.Seconds()
}

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter enforces one Rule across many keys (an IP, an email, a user id).
// Safe for concurrent use.
type Limiter struct {
	rule Rule
	now  func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// New builds a Limiter. It panics on a nonsensical rule: rules are compile-time
// constants in this codebase, so a bad one is a programming error that should
// surface at startup rather than silently disable the limit.
func New(r Rule) *Limiter {
	if r.N <= 0 || r.Per <= 0 {
		panic(fmt.Sprintf("ratelimit: invalid rule %+v (N and Per must be positive)", r))
	}
	return &Limiter{rule: r, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow reports whether an event for key is permitted, spending one token if so.
// When it is not permitted, the second return value is how long the caller must
// wait before a token is available — suitable for a Retry-After header.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()
	burst := l.rule.burst()
	rate := l.rule.ratePerSecond()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: burst, last: now}
		l.buckets[key] = b
	} else {
		// Refill for the elapsed time, capped at the burst size.
		if elapsed := now.Sub(b.last); elapsed > 0 {
			b.tokens += elapsed.Seconds() * rate
			if b.tokens > burst {
				b.tokens = burst
			}
		}
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / rate * float64(time.Second))
	return false, wait
}

// Sweep drops buckets that have sat idle long enough to have fully refilled.
// Discarding a full bucket is indistinguishable from keeping it — a key that
// returns later gets a fresh full bucket either way — so this is free to do and
// is what stops the map growing without bound as unique IPs and addresses
// accumulate over the process's lifetime.
func (l *Limiter) Sweep() {
	cutoff := l.now().Add(-2 * l.rule.Per)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// len reports the number of live buckets. Used by tests to prove Sweep works.
func (l *Limiter) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Registry owns a set of named Limiters and the single goroutine that sweeps
// them all, so callers do not each have to run a ticker.
type Registry struct {
	mu       sync.RWMutex
	limiters map[string]*Limiter
}

func NewRegistry() *Registry {
	return &Registry{limiters: map[string]*Limiter{}}
}

// Add registers a Limiter under name and returns it. It panics on a duplicate
// name, which would otherwise silently give two call sites separate buckets
// they both believed were shared.
func (r *Registry) Add(name string, rule Rule) *Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.limiters[name]; ok {
		panic("ratelimit: duplicate limiter name " + name)
	}
	l := New(rule)
	r.limiters[name] = l
	return l
}

// Get returns the named Limiter, or nil if it was never added.
func (r *Registry) Get(name string) *Limiter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.limiters[name]
}

// Sweep sweeps every registered Limiter.
func (r *Registry) Sweep() {
	r.mu.RLock()
	ls := make([]*Limiter, 0, len(r.limiters))
	for _, l := range r.limiters {
		ls = append(ls, l)
	}
	r.mu.RUnlock()
	for _, l := range ls {
		l.Sweep()
	}
}

// Run sweeps every interval until ctx is cancelled.
func (r *Registry) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Sweep()
		}
	}
}
