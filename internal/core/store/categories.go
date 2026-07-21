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

func (s *Store) CreateGroup(ctx context.Context, userID int64, name string, sortOrder int64) (int64, error) {
	id, err := s.insertReturningID(ctx,
		`INSERT INTO category_groups(user_id, name, sort_order) VALUES ($1, $2, $3)`, userID, name, sortOrder)
	if err != nil {
		return 0, fmt.Errorf("create group: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateGroup(ctx context.Context, userID int64, g CategoryGroup) error {
	_, err := s.run(ctx,
		`UPDATE category_groups SET name=$1, sort_order=$2 WHERE id=$3 AND user_id=$4`, g.Name, g.SortOrder, g.ID, userID)
	return err
}

// DeleteGroup removes a category group. A group may still own archived
// categories (soft-deleted from the budget page); those linger with their
// group_id and would otherwise trip the categories→category_groups foreign key.
// They are cleaned up in the same transaction. The delete is rejected if the
// group still has an active category, or an archived category that carries
// history (referenced by a transaction or an account's payment category) — the
// foreign key rolls the whole delete back so no history is lost.
func (s *Store) DeleteGroup(ctx context.Context, userID, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var active int
	if err := s.txQueryOne(ctx, tx,
		`SELECT COUNT(*) FROM categories WHERE group_id=$1 AND archived_at IS NULL AND user_id=$2`, id, userID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("group is not empty")
	}

	if _, err := s.txExec(ctx, tx,
		`DELETE FROM categories WHERE group_id=$1 AND archived_at IS NOT NULL AND user_id=$2`, id, userID); err != nil {
		return fmt.Errorf("group has categories with history and cannot be deleted: %w", err)
	}
	if _, err := s.txExec(ctx, tx, `DELETE FROM category_groups WHERE id=$1 AND user_id=$2`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListGroups(ctx context.Context, userID int64) ([]CategoryGroup, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, name, sort_order FROM category_groups WHERE user_id=$1 ORDER BY sort_order, name`, userID)
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
func (s *Store) MaxGroupSortOrder(ctx context.Context, userID int64) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM category_groups WHERE user_id=$1`, userID).Scan(&max)
	return max, err
}

// MinGroupSortOrder returns the smallest sort_order among category groups, or 0
// if there are none. Callers subtract 1 to prepend a new group at the top.
func (s *Store) MinGroupSortOrder(ctx context.Context, userID int64) (int64, error) {
	var min int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MIN(sort_order), 0) FROM category_groups WHERE user_id=$1`, userID).Scan(&min)
	return min, err
}

// ReorderGroups persists a new top-to-bottom ordering of the user's category
// groups by setting each group's sort_order to its index in ids. Groups absent
// from ids (e.g. the system Income group, which the budget table hides) keep
// their existing sort_order. It runs in one transaction so the order applies
// atomically, and each UPDATE is scoped to the user so an id owned by someone
// else is a silent no-op.
func (s *Store) ReorderGroups(ctx context.Context, userID int64, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range ids {
		if _, err := s.txExec(ctx, tx,
			`UPDATE category_groups SET sort_order=$1 WHERE id=$2 AND user_id=$3`, int64(i), id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReorderCategories persists a group's category order (and, for a cross-group
// drag, the move) by setting each listed category's group_id to groupID and its
// sort_order to its index in catIDs. The destination group is passed once with
// its full new membership; a cross-group move is two calls (destination gains
// the moved category, source loses it). It validates the target group is the
// user's, runs in one transaction, and scopes each UPDATE to the user so a
// foreign category id is a silent no-op.
func (s *Store) ReorderCategories(ctx context.Context, userID, groupID int64, catIDs []int64) error {
	if err := s.requireGroup(ctx, userID, groupID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range catIDs {
		if _, err := s.txExec(ctx, tx,
			`UPDATE categories SET group_id=$1, sort_order=$2 WHERE id=$3 AND user_id=$4`,
			groupID, int64(i), id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MaxCategorySortOrder returns the largest sort_order among categories in a
// group, or 0 if the group has none. Callers add 1 to append.
func (s *Store) MaxCategorySortOrder(ctx context.Context, userID, groupID int64) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM categories WHERE group_id=$1 AND user_id=$2`, groupID, userID).Scan(&max)
	return max, err
}

// --- Categories ---

func (s *Store) CreateCategory(ctx context.Context, userID int64, c Category) (int64, error) {
	var due sql.NullTime
	if c.GoalDueDate != nil {
		due = sql.NullTime{Time: *c.GoalDueDate, Valid: true}
	}
	if c.RolloverMode == "" {
		c.RolloverMode = RolloverCarryPositive
	}
	if err := s.requireGroup(ctx, userID, c.GroupID); err != nil {
		return 0, err
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO categories(user_id, group_id, name, goal_cents, goal_due_date, sort_order, rollover_mode)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID, c.GroupID, c.Name, nullInt(c.GoalCents), due, c.SortOrder, c.RolloverMode)
	if err != nil {
		return 0, fmt.Errorf("create category: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateCategory(ctx context.Context, userID int64, c Category) error {
	var due sql.NullTime
	if c.GoalDueDate != nil {
		due = sql.NullTime{Time: *c.GoalDueDate, Valid: true}
	}
	// UpdateCategory can move a category between groups; the destination group
	// must belong to the acting user.
	if err := s.requireGroup(ctx, userID, c.GroupID); err != nil {
		return err
	}
	_, err := s.run(ctx,
		`UPDATE categories
		 SET group_id=$1, name=$2, goal_cents=$3, goal_due_date=$4, sort_order=$5, rollover_mode=$6
		 WHERE id=$7 AND user_id=$8`,
		c.GroupID, c.Name, nullInt(c.GoalCents), due, c.SortOrder, c.RolloverMode, c.ID, userID)
	return err
}

func (s *Store) ArchiveCategory(ctx context.Context, userID, id int64) error {
	if err := s.checkNotIncome(ctx, userID, id); err != nil {
		return err
	}
	_, err := s.run(ctx,
		`UPDATE categories SET archived_at=CURRENT_TIMESTAMP WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

func (s *Store) DeleteCategory(ctx context.Context, userID, id int64) error {
	if err := s.checkNotIncome(ctx, userID, id); err != nil {
		return err
	}
	_, err := s.run(ctx, `DELETE FROM categories WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

func (s *Store) checkNotIncome(ctx context.Context, userID, id int64) error {
	var isIncome bool
	if err := s.queryOne(ctx,
		`SELECT is_income FROM categories WHERE id=$1 AND user_id=$2`, id, userID).Scan(&isIncome); err != nil {
		return err
	}
	if isIncome {
		return fmt.Errorf("the Income category is system-managed and cannot be modified")
	}
	return nil
}

// ListCategories returns active categories.
func (s *Store) ListCategories(ctx context.Context, userID int64, includeArchived bool) ([]Category, error) {
	q := `SELECT id, group_id, name, goal_cents, goal_due_date, sort_order, archived_at, is_income, rollover_mode FROM categories WHERE user_id=$1`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY sort_order, name`
	rows, err := s.queryAll(ctx, q, userID)
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
func (s *Store) SetRolloverMode(ctx context.Context, userID, categoryID int64, mode string) error {
	_, err := s.run(ctx,
		`UPDATE categories SET rollover_mode=$1 WHERE id=$2 AND user_id=$3`, mode, categoryID, userID)
	return err
}
