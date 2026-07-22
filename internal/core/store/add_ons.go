package store

import (
	"context"
	"errors"
	"fmt"
)

// ErrAddOnNotFound is returned when a slug doesn't match any catalog add-on.
var ErrAddOnNotFound = errors.New("add-on not found")

// AddOn is one catalog add-on plus whether the acting user has it enabled.
// PriceCents is a monthly price in integer cents; 0 means free.
type AddOn struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	PriceCents  int64
	Enabled     bool
}

// ListAddOnsForUser returns the full add-on catalog, each annotated with the
// user's enabled state (false when the user has no user_add_ons row yet).
func (s *Store) ListAddOnsForUser(ctx context.Context, userID int64) ([]AddOn, error) {
	rows, err := s.queryAll(ctx,
		`SELECT a.id, a.slug, a.name, a.description, a.price_cents,
		        COALESCE(ua.enabled, FALSE)
		 FROM add_ons a
		 LEFT JOIN user_add_ons ua ON ua.add_on_id = a.id AND ua.user_id = $1
		 ORDER BY a.name`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []AddOn
	for rows.Next() {
		var a AddOn
		if err := rows.Scan(&a.ID, &a.Slug, &a.Name, &a.Description, &a.PriceCents, &a.Enabled); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// EnabledAddOnSlugs returns the slugs the user currently has enabled. Used to
// gate navigation and routes.
func (s *Store) EnabledAddOnSlugs(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.queryAll(ctx,
		`SELECT a.slug
		 FROM user_add_ons ua
		 JOIN add_ons a ON a.id = ua.add_on_id
		 WHERE ua.user_id = $1 AND ua.enabled = TRUE`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// SetAddOnEnabled turns the add-on identified by slug on or off for the user.
// The row is inserted the first time and its enabled flag toggled thereafter
// (never churned). Returns ErrAddOnNotFound when slug isn't in the catalog.
func (s *Store) SetAddOnEnabled(ctx context.Context, userID int64, slug string, enabled bool) error {
	res, err := s.run(ctx,
		`INSERT INTO user_add_ons (user_id, add_on_id, enabled)
		 SELECT $1, id, $3 FROM add_ons WHERE slug = $2
		 ON CONFLICT (user_id, add_on_id)
		 DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = CURRENT_TIMESTAMP`,
		userID, slug, enabled)
	if err != nil {
		return fmt.Errorf("set add-on %q: %w", slug, err)
	}
	// Zero rows affected means the slug matched no add_ons row (the SELECT was
	// empty), so nothing was inserted or updated.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrAddOnNotFound
	}
	return nil
}
