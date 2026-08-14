package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoCredential means no passkey matches.
var ErrNoCredential = errors.New("no such passkey")

// WebAuthnCredential is one registered passkey.
type WebAuthnCredential struct {
	ID              int64
	UserID          int64
	CredentialID    []byte
	PublicKey       []byte
	AAGUID          []byte
	SignCount       int64
	Transports      string
	AttestationType string
	// BackupEligible reports whether the authenticator may sync this credential
	// to a cloud keychain; BackupState whether it currently is. A passkey that
	// cannot be backed up dies with its device, which is worth surfacing.
	BackupEligible bool
	BackupState    bool
	// CloneWarning latches when a sign counter goes backwards — the documented
	// signal that a credential has been duplicated.
	CloneWarning bool
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

const credentialColumns = `id, user_id, credential_id, public_key, aaguid, sign_count,
	transports, attestation_type, backup_eligible, backup_state, clone_warning,
	name, created_at, last_used_at`

func (s *Store) CreateCredential(ctx context.Context, c WebAuthnCredential) error {
	_, err := s.run(ctx,
		`INSERT INTO webauthn_credentials
		   (user_id, credential_id, public_key, aaguid, sign_count, transports,
		    attestation_type, backup_eligible, backup_state, name)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		c.UserID, c.CredentialID, c.PublicKey, c.AAGUID, c.SignCount, c.Transports,
		c.AttestationType, c.BackupEligible, c.BackupState, c.Name)
	if err != nil {
		return fmt.Errorf("create credential: %w", err)
	}
	return nil
}

// GetCredential looks one up by the authenticator's own handle, which is what
// an assertion carries.
func (s *Store) GetCredential(ctx context.Context, credentialID []byte) (WebAuthnCredential, error) {
	return s.scanCredential(s.queryOne(ctx,
		`SELECT `+credentialColumns+` FROM webauthn_credentials WHERE credential_id = $1`,
		credentialID))
}

func (s *Store) scanCredential(row *sql.Row) (WebAuthnCredential, error) {
	var c WebAuthnCredential
	var lastUsed nullTime
	err := row.Scan(&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.AAGUID, &c.SignCount,
		&c.Transports, &c.AttestationType, &c.BackupEligible, &c.BackupState, &c.CloneWarning,
		&c.Name, &c.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return WebAuthnCredential{}, ErrNoCredential
	}
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("scan credential: %w", err)
	}
	c.LastUsedAt = lastUsed.Ptr()
	return c, nil
}

// ListCredentialsForUser returns the user's passkeys, newest first.
func (s *Store) ListCredentialsForUser(ctx context.Context, userID int64) ([]WebAuthnCredential, error) {
	rows, err := s.queryAll(ctx,
		`SELECT `+credentialColumns+` FROM webauthn_credentials
		 WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()

	var out []WebAuthnCredential
	for rows.Next() {
		var c WebAuthnCredential
		var lastUsed nullTime
		if err := rows.Scan(&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.AAGUID, &c.SignCount,
			&c.Transports, &c.AttestationType, &c.BackupEligible, &c.BackupState, &c.CloneWarning,
			&c.Name, &c.CreatedAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan credential row: %w", err)
		}
		c.LastUsedAt = lastUsed.Ptr()
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecordCredentialUse updates the counter after a successful assertion.
//
// cloned latches permanently: a counter that has gone backwards once is
// evidence about the credential, not about this particular sign-in, and
// clearing it on the next well-behaved assertion would hide exactly the case
// worth keeping.
func (s *Store) RecordCredentialUse(ctx context.Context, credentialID []byte, signCount int64, cloned bool) error {
	_, err := s.run(ctx,
		`UPDATE webauthn_credentials
		 SET sign_count = $2,
		     clone_warning = clone_warning OR $3,
		     last_used_at = CURRENT_TIMESTAMP
		 WHERE credential_id = $1`,
		credentialID, signCount, cloned)
	if err != nil {
		return fmt.Errorf("record credential use: %w", err)
	}
	return nil
}

// RenameCredential relabels one of the user's passkeys.
func (s *Store) RenameCredential(ctx context.Context, userID, id int64, name string) (bool, error) {
	res, err := s.run(ctx,
		`UPDATE webauthn_credentials SET name = $3 WHERE id = $1 AND user_id = $2`,
		id, userID, name)
	if err != nil {
		return false, fmt.Errorf("rename credential: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rename credential rows: %w", err)
	}
	return n > 0, nil
}

// DeleteCredentialForUser removes one passkey, scoped to its owner in the same
// statement so removing someone else's is impossible rather than merely checked.
func (s *Store) DeleteCredentialForUser(ctx context.Context, userID, id int64) (bool, error) {
	res, err := s.run(ctx,
		`DELETE FROM webauthn_credentials WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete credential: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete credential rows: %w", err)
	}
	return n > 0, nil
}

func (s *Store) CountCredentials(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := s.queryOne(ctx,
		`SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = $1`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count credentials: %w", err)
	}
	return n, nil
}

// EnsureWebAuthnHandle returns the user's WebAuthn handle, minting one on first
// use. Stable for the life of the account: it is written into every
// authenticator that registers a passkey, and changing it would orphan them all.
func (s *Store) EnsureWebAuthnHandle(ctx context.Context, userID int64) ([]byte, error) {
	var handle []byte
	err := s.queryOne(ctx, `SELECT webauthn_handle FROM users WHERE id = $1`, userID).Scan(&handle)
	if err != nil {
		return nil, fmt.Errorf("get webauthn handle: %w", err)
	}
	if len(handle) > 0 {
		return handle, nil
	}
	handle = make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return nil, fmt.Errorf("generate webauthn handle: %w", err)
	}
	// Guarded so two concurrent first-registrations cannot each mint a handle
	// and have the second overwrite credentials keyed to the first.
	res, err := s.run(ctx,
		`UPDATE users SET webauthn_handle = $2 WHERE id = $1 AND webauthn_handle IS NULL`,
		userID, handle)
	if err != nil {
		return nil, fmt.Errorf("set webauthn handle: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		// Someone else won; use theirs.
		if err := s.queryOne(ctx, `SELECT webauthn_handle FROM users WHERE id = $1`, userID).Scan(&handle); err != nil {
			return nil, fmt.Errorf("reread webauthn handle: %w", err)
		}
	}
	return handle, nil
}

// GetUserByWebAuthnHandle resolves the handle an assertion carries back to its
// user. Needed for discoverable sign-in, where the authenticator names the user
// rather than the other way round.
func (s *Store) GetUserByWebAuthnHandle(ctx context.Context, handle []byte) (User, error) {
	return s.scanUser(s.queryOne(ctx,
		`SELECT `+userColumns+` FROM users WHERE webauthn_handle = $1`, handle))
}
