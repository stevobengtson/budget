package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ChallengeKind names a sign-in state machine. Later phases add their own.
type ChallengeKind string

const KindMFA ChallengeKind = "mfa"

// ErrChallengeInvalid means the challenge is unknown, expired, or already used.
// The three are deliberately indistinguishable to callers: telling them apart
// would let someone probe which challenge tokens ever existed.
var ErrChallengeInvalid = errors.New("challenge is invalid or expired")

// AuthChallenge is sign-in state held between proving one factor and issuing a
// session.
type AuthChallenge struct {
	ID            int64
	UserID        int64
	TokenHash     string
	Kind          ChallengeKind
	Methods       string
	CodeHash      string
	CodeExpiresAt *time.Time
	Data          []byte
	Attempts      int
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

const challengeColumns = `id, user_id, token_hash, kind, methods, code_hash,
	code_expires_at, data, attempts, expires_at, created_at`

// CreateChallenge stores a new challenge. Only the token's hash is persisted,
// matching how sessions and verification tokens are handled: a database read
// never yields a usable credential.
func (s *Store) CreateChallenge(ctx context.Context, userID int64, tokenHash string, kind ChallengeKind, methods string, expiresAt time.Time, info SessionInfo) error {
	_, err := s.run(ctx,
		`INSERT INTO auth_challenges(user_id, token_hash, kind, methods, expires_at, user_agent, ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID, tokenHash, string(kind), methods, expiresAt, info.UserAgent, info.IP)
	if err != nil {
		return fmt.Errorf("create challenge: %w", err)
	}
	return nil
}

// GetChallenge returns a live, unconsumed challenge of the given kind.
func (s *Store) GetChallenge(ctx context.Context, tokenHash string, kind ChallengeKind) (AuthChallenge, error) {
	var c AuthChallenge
	var userID sql.NullInt64
	var codeHash sql.NullString
	var codeExpires nullTime
	var kindStr string
	err := s.queryOne(ctx,
		`SELECT `+challengeColumns+` FROM auth_challenges
		 WHERE token_hash = $1 AND kind = $2
		   AND consumed_at IS NULL AND expires_at > CURRENT_TIMESTAMP`,
		tokenHash, string(kind)).
		Scan(&c.ID, &userID, &c.TokenHash, &kindStr, &c.Methods, &codeHash,
			&codeExpires, &c.Data, &c.Attempts, &c.ExpiresAt, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthChallenge{}, ErrChallengeInvalid
	}
	if err != nil {
		return AuthChallenge{}, fmt.Errorf("get challenge: %w", err)
	}
	c.UserID = userID.Int64
	c.Kind = ChallengeKind(kindStr)
	c.CodeHash = codeHash.String
	c.CodeExpiresAt = codeExpires.Ptr()
	return c, nil
}

// SetChallengeCode attaches a freshly emailed one-time code, replacing any
// previous one and resetting the attempt counter — a resent code starts with a
// full allowance, or asking for a new one would inherit the old one's failures.
func (s *Store) SetChallengeCode(ctx context.Context, tokenHash, codeHash string, expiresAt time.Time) error {
	_, err := s.run(ctx,
		`UPDATE auth_challenges SET code_hash = $2, code_expires_at = $3, attempts = 0
		 WHERE token_hash = $1 AND consumed_at IS NULL`,
		tokenHash, codeHash, expiresAt)
	if err != nil {
		return fmt.Errorf("set challenge code: %w", err)
	}
	return nil
}

// IncrementChallengeAttempts counts a wrong guess and returns the new total.
func (s *Store) IncrementChallengeAttempts(ctx context.Context, tokenHash string) (int, error) {
	var attempts int
	err := s.queryOne(ctx,
		`UPDATE auth_challenges SET attempts = attempts + 1
		 WHERE token_hash = $1 AND consumed_at IS NULL
		 RETURNING attempts`, tokenHash).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrChallengeInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("increment challenge attempts: %w", err)
	}
	return attempts, nil
}

// ConsumeChallenge marks the challenge used and returns its user, in one
// statement so two concurrent submissions of the same correct code cannot both
// succeed and mint two sessions.
func (s *Store) ConsumeChallenge(ctx context.Context, tokenHash string, kind ChallengeKind) (int64, error) {
	var userID sql.NullInt64
	err := s.queryOne(ctx,
		`UPDATE auth_challenges SET consumed_at = CURRENT_TIMESTAMP
		 WHERE token_hash = $1 AND kind = $2
		   AND consumed_at IS NULL AND expires_at > CURRENT_TIMESTAMP
		 RETURNING user_id`, tokenHash, string(kind)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrChallengeInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("consume challenge: %w", err)
	}
	return userID.Int64, nil
}

// DeleteChallenge removes a challenge outright. Used when the attempt allowance
// is exhausted and when the user abandons the sign-in.
func (s *Store) DeleteChallenge(ctx context.Context, tokenHash string) error {
	_, err := s.run(ctx, `DELETE FROM auth_challenges WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) PruneExpiredChallenges(ctx context.Context) error {
	_, err := s.run(ctx, `DELETE FROM auth_challenges WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}
