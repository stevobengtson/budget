package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrTokenInvalid is returned when a verification token is missing, already
// consumed, expired, or of the wrong purpose.
var ErrTokenInvalid = errors.New("token invalid or expired")

func (s *Store) CreateVerificationToken(ctx context.Context, userID int64, tokenHash, purpose string, expiresAt time.Time) error {
	_, err := s.run(ctx,
		`INSERT INTO verification_tokens(user_id, token_hash, purpose, expires_at)
		 VALUES ($1, $2, $3, $4)`, userID, tokenHash, purpose, expiresAt)
	if err != nil {
		return fmt.Errorf("create verification token: %w", err)
	}
	return nil
}

// ConsumeToken validates a token by hash+purpose and marks it consumed in one
// transaction. Returns the owning user id. Single-use and expiry are enforced.
func (s *Store) ConsumeToken(ctx context.Context, tokenHash, purpose string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	var expiresAt time.Time
	var consumed nullTime
	err = s.txQueryOne(ctx, tx,
		`SELECT user_id, expires_at, consumed_at FROM verification_tokens
		 WHERE token_hash = $1 AND purpose = $2`, tokenHash, purpose).
		Scan(&userID, &expiresAt, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrTokenInvalid
	}
	if err != nil {
		return 0, err
	}
	if consumed.Valid || time.Now().After(expiresAt) {
		return 0, ErrTokenInvalid
	}
	if _, err := s.txExec(ctx, tx,
		`UPDATE verification_tokens SET consumed_at = CURRENT_TIMESTAMP
		 WHERE token_hash = $1`, tokenHash); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Store) PruneExpiredTokens(ctx context.Context) error {
	_, err := s.run(ctx, `DELETE FROM verification_tokens WHERE expires_at < CURRENT_TIMESTAMP`)
	return err
}
