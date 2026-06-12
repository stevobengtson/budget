package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type CategoryGroup struct {
	ID        int64
	Name      string
	SortOrder int64
}

type Category struct {
	ID          int64
	GroupID     int64
	Name        string
	GoalCents   *int64
	GoalDueDate *time.Time
	SortOrder   int64
	ArchivedAt  *time.Time
	// IsIncome flags a system-managed Income category. The TUI hides edit
	// and delete actions on these rows. Inflows categorized here are
	// summed in the budget banner as "actual income".
	IsIncome bool
}

// --- Groups ---

func (s *Store) CreateGroup(ctx context.Context, name string, sortOrder int64) (int64, error) {
	id, err := s.insertReturningID(ctx,
		`INSERT INTO category_groups(name, sort_order) VALUES (?, ?)`, name, sortOrder)
	if err != nil {
		return 0, fmt.Errorf("create group: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateGroup(ctx context.Context, g CategoryGroup) error {
	_, err := s.run(ctx,
		`UPDATE category_groups SET name=?, sort_order=? WHERE id=?`, g.Name, g.SortOrder, g.ID)
	return err
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	_, err := s.run(ctx, `DELETE FROM category_groups WHERE id=?`, id)
	return err
}

func (s *Store) ListGroups(ctx context.Context) ([]CategoryGroup, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, name, sort_order FROM category_groups ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []CategoryGroup
	for rows.Next() {
		var g CategoryGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// --- Categories ---

func (s *Store) CreateCategory(ctx context.Context, c Category) (int64, error) {
	var due sql.NullTime
	if c.GoalDueDate != nil {
		due = sql.NullTime{Time: *c.GoalDueDate, Valid: true}
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO categories(group_id, name, goal_cents, goal_due_date, sort_order)
		 VALUES (?, ?, ?, ?, ?)`,
		c.GroupID, c.Name, nullInt(c.GoalCents), due, c.SortOrder)
	if err != nil {
		return 0, fmt.Errorf("create category: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateCategory(ctx context.Context, c Category) error {
	var due sql.NullTime
	if c.GoalDueDate != nil {
		due = sql.NullTime{Time: *c.GoalDueDate, Valid: true}
	}
	_, err := s.run(ctx,
		`UPDATE categories
		 SET group_id=?, name=?, goal_cents=?, goal_due_date=?, sort_order=?
		 WHERE id=?`,
		c.GroupID, c.Name, nullInt(c.GoalCents), due, c.SortOrder, c.ID)
	return err
}

func (s *Store) ArchiveCategory(ctx context.Context, id int64) error {
	if err := s.checkNotIncome(ctx, id); err != nil {
		return err
	}
	_, err := s.run(ctx,
		`UPDATE categories SET archived_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	if err := s.checkNotIncome(ctx, id); err != nil {
		return err
	}
	_, err := s.run(ctx, `DELETE FROM categories WHERE id=?`, id)
	return err
}

func (s *Store) checkNotIncome(ctx context.Context, id int64) error {
	var isIncome bool
	if err := s.queryOne(ctx,
		`SELECT is_income FROM categories WHERE id=?`, id).Scan(&isIncome); err != nil {
		return err
	}
	if isIncome {
		return fmt.Errorf("the Income category is system-managed and cannot be modified")
	}
	return nil
}

// ListCategories returns active categories.
func (s *Store) ListCategories(ctx context.Context, includeArchived bool) ([]Category, error) {
	q := `SELECT id, group_id, name, goal_cents, goal_due_date, sort_order, archived_at, is_income FROM categories`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY sort_order, name`
	rows, err := s.queryAll(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Category
	for rows.Next() {
		var c Category
		var goal sql.NullInt64
		var due, archived nullTime
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &goal, &due, &c.SortOrder, &archived, &c.IsIncome); err != nil {
			return nil, err
		}
		c.GoalCents = intPtr(goal)
		c.GoalDueDate = due.Ptr()
		c.ArchivedAt = archived.Ptr()
		out = append(out, c)
	}
	return out, rows.Err()
}
