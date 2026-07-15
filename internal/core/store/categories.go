package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Rollover modes controlling how a category's leftover/overspend carries
// between months. Stored in categories.rollover_mode.
const (
	RolloverNone          = "none"           // isolated month: available = assigned − spent
	RolloverCarry         = "carry"          // carry leftovers and overspend forward
	RolloverCarryPositive = "carry_positive" // carry leftovers, reset overspend to 0 (default)
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
	// RolloverMode is one of RolloverNone / RolloverCarry / RolloverCarryPositive.
	RolloverMode string
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

// DeleteGroup removes a category group. A group may still own archived
// categories (soft-deleted from the budget page); those linger with their
// group_id and would otherwise trip the categories→category_groups foreign key.
// They are cleaned up in the same transaction. The delete is rejected if the
// group still has an active category, or an archived category that carries
// history (referenced by a transaction or an account's payment category) — the
// foreign key rolls the whole delete back so no history is lost.
func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var active int
	if err := s.txQueryOne(ctx, tx,
		`SELECT COUNT(*) FROM categories WHERE group_id=? AND archived_at IS NULL`, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("group is not empty")
	}

	if _, err := s.txExec(ctx, tx,
		`DELETE FROM categories WHERE group_id=? AND archived_at IS NOT NULL`, id); err != nil {
		return fmt.Errorf("group has categories with history and cannot be deleted: %w", err)
	}
	if _, err := s.txExec(ctx, tx, `DELETE FROM category_groups WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
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

// MaxGroupSortOrder returns the largest sort_order among category groups, or 0
// if there are none. Callers add 1 to append a new group at the end.
func (s *Store) MaxGroupSortOrder(ctx context.Context) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM category_groups`).Scan(&max)
	return max, err
}

// MaxCategorySortOrder returns the largest sort_order among categories in a
// group, or 0 if the group has none. Callers add 1 to append.
func (s *Store) MaxCategorySortOrder(ctx context.Context, groupID int64) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM categories WHERE group_id=?`, groupID).Scan(&max)
	return max, err
}

// --- Categories ---

func (s *Store) CreateCategory(ctx context.Context, c Category) (int64, error) {
	var due sql.NullTime
	if c.GoalDueDate != nil {
		due = sql.NullTime{Time: *c.GoalDueDate, Valid: true}
	}
	if c.RolloverMode == "" {
		c.RolloverMode = RolloverCarryPositive
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO categories(group_id, name, goal_cents, goal_due_date, sort_order, rollover_mode)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.GroupID, c.Name, nullInt(c.GoalCents), due, c.SortOrder, c.RolloverMode)
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
		 SET group_id=?, name=?, goal_cents=?, goal_due_date=?, sort_order=?, rollover_mode=?
		 WHERE id=?`,
		c.GroupID, c.Name, nullInt(c.GoalCents), due, c.SortOrder, c.RolloverMode, c.ID)
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
	q := `SELECT id, group_id, name, goal_cents, goal_due_date, sort_order, archived_at, is_income, rollover_mode FROM categories`
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
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &goal, &due, &c.SortOrder, &archived, &c.IsIncome, &c.RolloverMode); err != nil {
			return nil, err
		}
		c.GoalCents = intPtr(goal)
		c.GoalDueDate = due.Ptr()
		c.ArchivedAt = archived.Ptr()
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetRolloverMode updates a category's rollover mode. mode must be one of the
// Rollover* constants; the DB CHECK constraint rejects anything else.
func (s *Store) SetRolloverMode(ctx context.Context, categoryID int64, mode string) error {
	_, err := s.run(ctx,
		`UPDATE categories SET rollover_mode=? WHERE id=?`, mode, categoryID)
	return err
}
