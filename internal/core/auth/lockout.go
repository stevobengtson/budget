package auth

import (
	"context"
	"time"

	emailtpl "github.com/sbengtson/budget/internal/core/mail/templates"
	"github.com/sbengtson/budget/internal/core/store"
)

// LockoutPolicy is the escalation curve applied to repeated password failures
// against one email address.
//
// The lock is deliberately *soft*: it blocks password attempts only. A passkey,
// magic link, or OAuth sign-in for the same account still works while it is in
// force. Without that, anyone who knows a victim's email address could lock
// them out of their own money on demand by spraying the one factor they do not
// need — turning a defence into a denial-of-service tool.
type LockoutPolicy struct {
	// Threshold is how many consecutive failures are tolerated before the
	// first lock. It is not 3: people genuinely mistype passwords, and a
	// too-eager lock trains users to reach for password reset every time.
	Threshold int
	// Base is the first lock's duration; each subsequent failure doubles it.
	Base time.Duration
	// Max caps the doubling.
	Max time.Duration
	// Window is how long a failure stays "recent". Failures older than this
	// no longer count toward the threshold.
	Window time.Duration
}

// DefaultLockoutPolicy is used when config supplies nothing.
var DefaultLockoutPolicy = LockoutPolicy{
	Threshold: 8,
	Base:      15 * time.Minute,
	Max:       24 * time.Hour,
	Window:    time.Hour,
}

func (p LockoutPolicy) withDefaults() LockoutPolicy {
	d := DefaultLockoutPolicy
	if p.Threshold > 0 {
		d.Threshold = p.Threshold
	}
	if p.Base > 0 {
		d.Base = p.Base
	}
	if p.Max > 0 {
		d.Max = p.Max
	}
	if p.Window > 0 {
		d.Window = p.Window
	}
	return d
}

// lockDuration maps a failure count to how long the account is locked.
// Failures at or below the threshold are free; past it, each additional failure
// doubles the previous duration up to Max.
//
//	threshold=8, base=15m:  8 -> 0, 9 -> 15m, 10 -> 30m, 11 -> 1h ... capped at Max
func (p LockoutPolicy) lockDuration(failures int) time.Duration {
	over := failures - p.Threshold
	if over <= 0 {
		return 0
	}
	d := p.Base
	for range over - 1 {
		d *= 2
		if d >= p.Max {
			return p.Max
		}
	}
	return d
}

// checkLockout reports how long the subject must wait before another password
// attempt is accepted, or zero when it may proceed.
func (s *Service) checkLockout(ctx context.Context, email string) (time.Duration, error) {
	l, err := s.store.GetLockout(ctx, store.ScopePasswordLogin, email)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	if !l.Locked(now) {
		return 0, nil
	}
	return l.LockedUntil.Sub(now), nil
}

// recordLoginFailure counts a failed password attempt and applies the policy's
// lock when the count warrants one. It is called for unknown addresses too:
// counting only real accounts would make "this address never locks" a reliable
// signal that it is not registered.
func (s *Service) recordLoginFailure(ctx context.Context, email string) error {
	p := s.cfg.Lockout.withDefaults()
	failures, err := s.store.RecordLoginFailure(ctx, store.ScopePasswordLogin, email, p.Window)
	if err != nil {
		return err
	}
	d := p.lockDuration(failures)
	if d == 0 {
		return nil
	}
	if err := s.store.LockSubject(ctx, store.ScopePasswordLogin, email, time.Now().Add(d)); err != nil {
		return err
	}
	s.sendLockoutAlert(ctx, email)
	return nil
}

// sendLockoutAlert warns the account holder that their address is being
// hammered. It is reached only at the moment a lock is applied — once the lock
// is in force, Login rejects before ever counting another failure, so this
// cannot be used to flood the victim's inbox.
//
// Failures are ignored rather than propagated: the lock itself is the security
// outcome, and a mail outage must not turn into a login error for everyone else.
func (s *Service) sendLockoutAlert(ctx context.Context, email string) {
	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return // no such account: there is nobody to warn
	}
	msg, err := emailtpl.LockoutAlert(s.localeFor(ctx, u.ID), s.baseURL+"/forgot")
	if err != nil {
		return
	}
	msg.To = u.Email
	_ = s.mailer.Send(ctx, msg)
}

// clearLoginFailures wipes the counter after a successful sign-in.
func (s *Service) clearLoginFailures(ctx context.Context, email string) error {
	return s.store.ClearLoginFailures(ctx, store.ScopePasswordLogin, email)
}
