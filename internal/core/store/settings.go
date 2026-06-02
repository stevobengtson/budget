package store

import (
	"context"
	"database/sql"
	"fmt"
)

// GetSetting returns the stored value for key. The bool is false when the
// key is not present.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.queryOne(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get setting %q: %w", key, err)
	}
	return v, true, nil
}

// SetSetting upserts the value for key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.run(ctx,
		`INSERT INTO app_settings(key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// DeleteSetting removes the key. Missing keys are not an error.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.run(ctx, `DELETE FROM app_settings WHERE key=?`, key)
	if err != nil {
		return fmt.Errorf("delete setting %q: %w", key, err)
	}
	return nil
}
