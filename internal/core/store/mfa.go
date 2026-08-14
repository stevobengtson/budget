package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoTOTP means the user has no authenticator enrolment at all.
var ErrNoTOTP = errors.New("no TOTP enrolment")

// TOTP is a user's authenticator-app enrolment. Secret is a crypto.Sealer blob;
// the store never sees the base32 value.
type TOTP struct {
	UserID       int64
	Secret       []byte
	ConfirmedAt  *time.Time
	LastUsedStep *int64
	CreatedAt    time.Time
}

// Confirmed reports whether enrolment was completed by proving a code. An
// unconfirmed row is setup-in-progress and must never count as an active
// factor, or abandoning the setup screen would lock the user out.
func (t TOTP) Confirmed() bool { return t.ConfirmedAt != nil }

// StartTOTPEnrolment stores (or replaces) an unconfirmed secret. Replacing is
// deliberate: restarting setup must invalidate the QR code already on screen,
// so an abandoned enrolment can never be completed later from a stale tab.
func (s *Store) StartTOTPEnrolment(ctx context.Context, userID int64, secret []byte) error {
	_, err := s.run(ctx,
		`INSERT INTO user_totp(user_id, secret, confirmed_at, last_used_step)
		 VALUES ($1, $2, NULL, NULL)
		 ON CONFLICT (user_id) DO UPDATE SET
		     secret = EXCLUDED.secret, confirmed_at = NULL, last_used_step = NULL,
		     created_at = CURRENT_TIMESTAMP`,
		userID, secret)
	if err != nil {
		return fmt.Errorf("start totp enrolment: %w", err)
	}
	return nil
}

func (s *Store) GetTOTP(ctx context.Context, userID int64) (TOTP, error) {
	var t TOTP
	var confirmed nullTime
	var lastStep sql.NullInt64
	err := s.queryOne(ctx,
		`SELECT user_id, secret, confirmed_at, last_used_step, created_at
		 FROM user_totp WHERE user_id = $1`, userID).
		Scan(&t.UserID, &t.Secret, &confirmed, &lastStep, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTP{}, ErrNoTOTP
	}
	if err != nil {
		return TOTP{}, fmt.Errorf("get totp: %w", err)
	}
	t.ConfirmedAt = confirmed.Ptr()
	if lastStep.Valid {
		t.LastUsedStep = &lastStep.Int64
	}
	return t, nil
}

// ConfirmTOTPStep completes enrolment and records the step that proved it, in
// one statement. Combining them closes the window where the proving code could
// be replayed before the first step was written.
func (s *Store) ConfirmTOTPStep(ctx context.Context, userID int64, step int64) error {
	_, err := s.run(ctx,
		`UPDATE user_totp SET confirmed_at = CURRENT_TIMESTAMP, last_used_step = $2
		 WHERE user_id = $1`, userID, step)
	if err != nil {
		return fmt.Errorf("confirm totp: %w", err)
	}
	return nil
}

// UseTOTPStep records a successfully used step, but only when it is newer than
// the one stored. The WHERE clause is the replay guard itself: two concurrent
// submissions of the same code cannot both update, so only one can be accepted.
// Reports whether this call was the one that claimed the step.
func (s *Store) UseTOTPStep(ctx context.Context, userID int64, step int64) (bool, error) {
	res, err := s.run(ctx,
		`UPDATE user_totp SET last_used_step = $2
		 WHERE user_id = $1 AND (last_used_step IS NULL OR last_used_step < $2)`,
		userID, step)
	if err != nil {
		return false, fmt.Errorf("use totp step: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("use totp step rows: %w", err)
	}
	return n > 0, nil
}

func (s *Store) DeleteTOTP(ctx context.Context, userID int64) error {
	_, err := s.run(ctx, `DELETE FROM user_totp WHERE user_id = $1`, userID)
	return err
}

// SetEmailOTPEnabled turns emailed sign-in codes on or off for a user.
func (s *Store) SetEmailOTPEnabled(ctx context.Context, userID int64, on bool) error {
	_, err := s.run(ctx,
		`UPDATE users SET email_otp_enabled = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
		userID, on)
	if err != nil {
		return fmt.Errorf("set email otp: %w", err)
	}
	return nil
}

func (s *Store) EmailOTPEnabled(ctx context.Context, userID int64) (bool, error) {
	var on bool
	if err := s.queryOne(ctx, `SELECT email_otp_enabled FROM users WHERE id = $1`, userID).Scan(&on); err != nil {
		return false, fmt.Errorf("email otp enabled: %w", err)
	}
	return on, nil
}

// ReplaceRecoveryCodes swaps the user's whole set in one transaction.
// Regenerating must invalidate every old code atomically — a partial replace
// would leave a window where both sets, or neither, are valid.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID int64, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recovery codes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := s.txExec(ctx, tx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("clear recovery codes: %w", err)
	}
	for _, h := range hashes {
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO recovery_codes(user_id, code_hash) VALUES ($1, $2)`, userID, h); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	return tx.Commit()
}

// ConsumeRecoveryCode burns a code, reporting whether it was valid and unused.
// The UPDATE is the check: a code cannot be spent twice even under concurrent
// submissions, because only one statement can move used_at off NULL.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID int64, codeHash string) (bool, error) {
	res, err := s.run(ctx,
		`UPDATE recovery_codes SET used_at = CURRENT_TIMESTAMP
		 WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`,
		userID, codeHash)
	if err != nil {
		return false, fmt.Errorf("consume recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume recovery code rows: %w", err)
	}
	return n > 0, nil
}

// CountUnusedRecoveryCodes drives the "N codes left" hint on the security page.
func (s *Store) CountUnusedRecoveryCodes(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.queryOne(ctx,
		`SELECT COUNT(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return n, nil
}

// DeleteRecoveryCodes drops the set, used when the last second factor is
// removed and the codes would otherwise linger with nothing to recover.
func (s *Store) DeleteRecoveryCodes(ctx context.Context, userID int64) error {
	_, err := s.run(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID)
	return err
}
