// Package settings exposes resolvers for app-wide user preferences. Values
// are persisted via store.GetSetting / store.SetSetting, but the resolvers
// here layer fallback semantics on top so callers always get a usable
// answer.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/sbengtson/budget/internal/core/store"
)

const (
	DefaultAccountKey  = "defaults.account_id"
	DefaultCategoryKey = "defaults.category_id"
)

// ErrNoAccounts is returned by ResolveDefaultAccount when the accounts table
// contains no non-archived rows.
var ErrNoAccounts = errors.New("no accounts available")

// ResolveDefaultAccount returns the account id to pre-select on the
// new-transaction form. If the user has set a default, returns that id when
// it still refers to a non-archived account. Otherwise returns the lowest-id
// non-archived account.
func ResolveDefaultAccount(ctx context.Context, s *store.Store) (int64, error) {
	if id, ok, err := readIDSetting(ctx, s, DefaultAccountKey); err != nil {
		return 0, err
	} else if ok {
		if valid, err := accountExistsActive(ctx, s, id); err != nil {
			return 0, err
		} else if valid {
			return id, nil
		}
	}
	id, ok, err := firstActiveAccountID(ctx, s)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrNoAccounts
	}
	return id, nil
}

// ResolveDefaultCategory mirrors ResolveDefaultAccount but returns nil when
// no categories exist (category is optional on a transaction).
func ResolveDefaultCategory(ctx context.Context, s *store.Store) (*int64, error) {
	if id, ok, err := readIDSetting(ctx, s, DefaultCategoryKey); err != nil {
		return nil, err
	} else if ok {
		if valid, err := categoryExistsActive(ctx, s, id); err != nil {
			return nil, err
		} else if valid {
			return &id, nil
		}
	}
	id, ok, err := firstActiveCategoryID(ctx, s)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func readIDSetting(ctx context.Context, s *store.Store, key string) (int64, bool, error) {
	v, ok, err := s.GetSetting(ctx, key)
	if err != nil || !ok || v == "" {
		return 0, false, err
	}
	id, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return 0, false, nil
	}
	return id, true, nil
}

func accountExistsActive(ctx context.Context, s *store.Store, id int64) (bool, error) {
	var x int64
	err := s.DB().QueryRowContext(ctx,
		s.Dialect().Rebind(`SELECT id FROM accounts WHERE id=? AND archived_at IS NULL`),
		id).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func categoryExistsActive(ctx context.Context, s *store.Store, id int64) (bool, error) {
	// Exclude the system-managed Income category seeded by migration 00005;
	// it is not a valid default for new transactions.
	var x int64
	err := s.DB().QueryRowContext(ctx,
		s.Dialect().Rebind(`SELECT id FROM categories WHERE id=? AND archived_at IS NULL AND is_income = ?`),
		id, false).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func firstActiveAccountID(ctx context.Context, s *store.Store) (int64, bool, error) {
	var id int64
	err := s.DB().QueryRowContext(ctx,
		`SELECT id FROM accounts WHERE archived_at IS NULL ORDER BY id ASC LIMIT 1`,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func firstActiveCategoryID(ctx context.Context, s *store.Store) (int64, bool, error) {
	// Exclude the system-managed Income category seeded by migration 00005;
	// it is not a valid default for new transactions.
	var id int64
	err := s.DB().QueryRowContext(ctx,
		s.Dialect().Rebind(`SELECT id FROM categories WHERE archived_at IS NULL AND is_income = ? ORDER BY id ASC LIMIT 1`),
		false).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
