package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoIdentity means no federated identity matches.
var ErrNoIdentity = errors.New("no such linked account")

// OAuthIdentity is one federated sign-in identity.
type OAuthIdentity struct {
	ID       int64
	UserID   int64
	Provider string
	// Subject is the provider's stable identifier for this person. It is the
	// key, not the email: Apple's Hide My Email hands out a relay address the
	// user can revoke at any time.
	Subject string
	// Email is what the provider reported, if anything. Apple sends it only on
	// the first authorization.
	Email         string
	EmailVerified bool
	CreatedAt     time.Time
	LastUsedAt    *time.Time
}

const identityColumns = `id, user_id, provider, subject, email, email_verified, created_at, last_used_at`

// LinkIdentity attaches a federated identity to an account.
func (s *Store) LinkIdentity(ctx context.Context, userID int64, provider, subject, email string, emailVerified bool) error {
	_, err := s.run(ctx,
		`INSERT INTO oauth_identities(user_id, provider, subject, email, email_verified)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID, provider, subject, nullableString(email), emailVerified)
	if err != nil {
		return fmt.Errorf("link identity: %w", err)
	}
	return nil
}

// GetIdentity finds a link by the provider's own identifier.
func (s *Store) GetIdentity(ctx context.Context, provider, subject string) (OAuthIdentity, error) {
	return s.scanIdentity(s.queryOne(ctx,
		`SELECT `+identityColumns+` FROM oauth_identities WHERE provider = $1 AND subject = $2`,
		provider, subject))
}

func (s *Store) scanIdentity(row *sql.Row) (OAuthIdentity, error) {
	var i OAuthIdentity
	var email sql.NullString
	var lastUsed nullTime
	err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &email, &i.EmailVerified,
		&i.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthIdentity{}, ErrNoIdentity
	}
	if err != nil {
		return OAuthIdentity{}, fmt.Errorf("scan identity: %w", err)
	}
	i.Email = email.String
	i.LastUsedAt = lastUsed.Ptr()
	return i, nil
}

// ListIdentitiesForUser returns the account's linked providers.
func (s *Store) ListIdentitiesForUser(ctx context.Context, userID int64) ([]OAuthIdentity, error) {
	rows, err := s.queryAll(ctx,
		`SELECT `+identityColumns+` FROM oauth_identities WHERE user_id = $1 ORDER BY provider`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	var out []OAuthIdentity
	for rows.Next() {
		var i OAuthIdentity
		var email sql.NullString
		var lastUsed nullTime
		if err := rows.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &email, &i.EmailVerified,
			&i.CreatedAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan identity row: %w", err)
		}
		i.Email = email.String
		i.LastUsedAt = lastUsed.Ptr()
		out = append(out, i)
	}
	return out, rows.Err()
}

// TouchIdentity records a sign-in, and fills in an address that was not known
// before.
//
// The backfill matters for Apple, which reports the email only on the very
// first authorization. If that first response was not stored — or the link was
// made before the address was known — this is the only chance to record it.
func (s *Store) TouchIdentity(ctx context.Context, provider, subject, email string, emailVerified bool) error {
	_, err := s.run(ctx,
		`UPDATE oauth_identities
		 SET last_used_at = CURRENT_TIMESTAMP,
		     email = COALESCE(NULLIF($3, ''), email),
		     email_verified = email_verified OR $4
		 WHERE provider = $1 AND subject = $2`,
		provider, subject, email, emailVerified)
	if err != nil {
		return fmt.Errorf("touch identity: %w", err)
	}
	return nil
}

// DeleteIdentityForUser unlinks a provider, scoped to its owner in the same
// statement so unlinking someone else's is impossible rather than checked.
func (s *Store) DeleteIdentityForUser(ctx context.Context, userID, id int64) (bool, error) {
	res, err := s.run(ctx,
		`DELETE FROM oauth_identities WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete identity: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete identity rows: %w", err)
	}
	return n > 0, nil
}

func (s *Store) CountIdentities(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.queryOne(ctx,
		`SELECT COUNT(*) FROM oauth_identities WHERE user_id = $1`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count identities: %w", err)
	}
	return n, nil
}

// nullableString stores "" as NULL, so an absent email is absent rather than
// an empty string that COALESCE would treat as a real value.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
