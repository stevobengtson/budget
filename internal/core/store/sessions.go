package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Session is one signed-in device. The mobile apps' bearer token is a row in
// this same table (see apiv1.RequireBearerAuth), so anything that lists or
// revokes sessions is acting on phones as well as browsers.
type Session struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	UserAgent string
	IP        string
	// LastSeenAt is refreshed lazily — see TouchSession.
	LastSeenAt time.Time
	// ReauthAt is the last time a factor was re-proved for step-up. nil means
	// never since creation, in which case CreatedAt is the effective value.
	ReauthAt *time.Time
	// Client is the surface that minted the session: "web", "ios", "android".
	Client string
	// Label is a human description of the device, frozen at creation.
	Label string
}

// AuthenticatedAt returns the time the session's factors were last proved: the
// step-up timestamp when there is one, else when the session was created.
func (s Session) AuthenticatedAt() time.Time {
	if s.ReauthAt != nil {
		return *s.ReauthAt
	}
	return s.CreatedAt
}

// sessionColumns is the SELECT list every session lookup shares, kept in one
// place so it cannot drift out of step with scanSession's Scan order.
const sessionColumns = `id, user_id, token_hash, expires_at, created_at,
	user_agent, ip, last_seen_at, reauth_at, client, label`

// SessionInfo is the provenance recorded with a new session. It is a struct
// rather than four positional strings because the four are interchangeable to
// the compiler: transposing IP and Client would store nonsense silently.
type SessionInfo struct {
	UserAgent string
	IP        string
	// Client is the surface that minted it. Empty means "web".
	Client string
	Label  string
}

func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time, info SessionInfo) error {
	client := info.Client
	if client == "" {
		client = "web"
	}
	_, err := s.run(ctx,
		`INSERT INTO sessions(user_id, token_hash, expires_at, user_agent, ip, client, label)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID, tokenHash, expiresAt, info.UserAgent, info.IP, client, info.Label)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	return s.scanSession(s.queryOne(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE token_hash = $1`, tokenHash))
}

func (s *Store) scanSession(row *sql.Row) (Session, error) {
	var sess Session
	var userAgent, ip, label sql.NullString
	var reauth nullTime
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt,
		&userAgent, &ip, &sess.LastSeenAt, &reauth, &sess.Client, &label); err != nil {
		return Session{}, err
	}
	sess.UserAgent = userAgent.String
	sess.IP = ip.String
	sess.Label = label.String
	sess.ReauthAt = reauth.Ptr()
	return sess, nil
}

// ListSessionsForUser returns the user's unexpired sessions, most recently seen
// first — the order the "active sessions" screen wants.
func (s *Store) ListSessionsForUser(ctx context.Context, userID int64) ([]Session, error) {
	rows, err := s.queryAll(ctx,
		`SELECT `+sessionColumns+` FROM sessions
		 WHERE user_id = $1 AND expires_at > CURRENT_TIMESTAMP
		 ORDER BY last_seen_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var sess Session
		var userAgent, ip, label sql.NullString
		var reauth nullTime
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt,
			&userAgent, &ip, &sess.LastSeenAt, &reauth, &sess.Client, &label); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.UserAgent = userAgent.String
		sess.IP = ip.String
		sess.Label = label.String
		sess.ReauthAt = reauth.Ptr()
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteSessionForUser revokes one session by its row id, scoped to its owner.
// Taking the id rather than the token hash means the revoke button never has to
// carry a live credential in the page, and scoping the DELETE by user_id in the
// same statement makes revoking someone else's session impossible rather than
// merely checked. Reports whether a row was actually removed.
func (s *Store) DeleteSessionForUser(ctx context.Context, userID, id int64) (bool, error) {
	res, err := s.run(ctx, `DELETE FROM sessions WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete session rows: %w", err)
	}
	return n > 0, nil
}

// DeleteSessionsForUserExcept revokes every session the user has apart from the
// one identified by keepTokenHash, and returns how many were removed. Pass an
// empty keepTokenHash to revoke all of them. Used after a password change (keep
// the current device signed in) and a password reset (keep nothing).
func (s *Store) DeleteSessionsForUserExcept(ctx context.Context, userID int64, keepTokenHash string) (int64, error) {
	res, err := s.run(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, keepTokenHash)
	if err != nil {
		return 0, fmt.Errorf("delete other sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete other sessions rows: %w", err)
	}
	return n, nil
}

// TouchSession refreshes last_seen_at, but only when it is already older than
// staleAfter. This runs on the authentication path of every request, so an
// unconditional UPDATE would make every page view a write on a hot table; the
// WHERE clause turns that into one write per session per staleAfter window.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, staleAfter time.Duration) error {
	_, err := s.run(ctx,
		`UPDATE sessions SET last_seen_at = CURRENT_TIMESTAMP
		 WHERE token_hash = $1 AND last_seen_at < CURRENT_TIMESTAMP - $2::interval`,
		tokenHash, staleAfter.String())
	return err
}

// MarkSessionReauthenticated records that the user just re-proved a factor,
// opening the step-up window for sensitive actions.
func (s *Store) MarkSessionReauthenticated(ctx context.Context, tokenHash string) error {
	_, err := s.run(ctx,
		`UPDATE sessions SET reauth_at = CURRENT_TIMESTAMP WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("mark reauthenticated: %w", err)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.run(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) PruneExpiredSessions(ctx context.Context) error {
	_, err := s.run(ctx, `DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}
