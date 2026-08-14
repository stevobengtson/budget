package auth

import (
	"context"
	"time"

	"github.com/sbengtson/budget/internal/core/store"
)

// touchAfter is how stale last_seen_at must be before a request rewrites it.
// Any value here trades precision in the sessions list for writes on the
// authentication path of every single request; a quarter hour is far more
// resolution than "when was this device last used" needs.
const touchAfter = 15 * time.Minute

// SessionView is one row of the active-sessions screen. It deliberately has no
// token hash: the UI addresses sessions by id, so a live credential never has
// to be rendered into a page.
type SessionView struct {
	ID       int64
	Label    string
	Client   string
	IP       string
	LastSeen time.Time
	Created  time.Time
	// Current marks the session making the request, which must be presented
	// differently — revoking it is signing yourself out.
	Current bool
}

// ListSessions returns the user's active sessions, flagging the caller's own.
func (s *Service) ListSessions(ctx context.Context, userID int64, currentRawToken string) ([]SessionView, error) {
	rows, err := s.store.ListSessionsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	currentHash := ""
	if currentRawToken != "" {
		currentHash = HashToken(currentRawToken)
	}
	out := make([]SessionView, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionView{
			ID:       r.ID,
			Label:    r.Label,
			Client:   r.Client,
			IP:       r.IP,
			LastSeen: r.LastSeenAt,
			Created:  r.CreatedAt,
			Current:  currentHash != "" && r.TokenHash == currentHash,
		})
	}
	return out, nil
}

// RevokeSession signs one device out. It reports ErrInvalidToken when the id is
// not one of this user's sessions, so a caller cannot distinguish "already gone"
// from "belongs to somebody else" — the store scopes the delete by user_id, so
// there is nothing to leak either way.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID int64) error {
	deleted, err := s.store.DeleteSessionForUser(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrInvalidToken
	}
	return nil
}

// RevokeOtherSessions signs out every device except the caller's own, and
// returns how many were signed out.
//
// This includes the user's phone: the mobile apps authenticate with a bearer
// token that is a row in this same table. That is the correct behaviour for
// "sign out everywhere", but it is surprising enough that the UI has to say so.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID int64, currentRawToken string) (int64, error) {
	keep := ""
	if currentRawToken != "" {
		keep = HashToken(currentRawToken)
	}
	return s.store.DeleteSessionsForUserExcept(ctx, userID, keep)
}

// TouchSession records that the session was used, subject to touchAfter.
// Best-effort by design: this runs on every authenticated request, and a failed
// bookkeeping write must never turn into a failed page load.
func (s *Service) TouchSession(ctx context.Context, rawToken string) {
	_ = s.store.TouchSession(ctx, HashToken(rawToken), touchAfter)
}

// SessionFor resolves a raw token to its stored row, for callers that need the
// session itself rather than its user (step-up checks).
func (s *Service) SessionFor(ctx context.Context, rawToken string) (store.Session, error) {
	sess, err := s.store.GetSessionByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return store.Session{}, ErrInvalidCredentials
	}
	return sess, nil
}
