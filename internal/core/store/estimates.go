package store

import (
	"context"
	"fmt"
	"time"
)

// Estimate is a named snapshot of the user's budget and income, editable
// independently of the real budget (Budget Estimate add-on).
type Estimate struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EstimateGroup struct {
	ID         int64
	EstimateID int64
	Name       string
	SortOrder  int64
}

type EstimateCategory struct {
	ID            int64
	GroupID       int64
	Name          string
	AssignedCents int64
	SortOrder     int64
}

type EstimateIncome struct {
	ID          int64
	EstimateID  int64
	Name        string
	AmountCents int64
	SortOrder   int64
}

func (s *Store) ownsEstimate(ctx context.Context, userID, id int64) (bool, error) {
	return s.rowExists(ctx, "estimates", userID, id)
}

func (s *Store) requireEstimate(ctx context.Context, userID, id int64) error {
	ok, err := s.ownsEstimate(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotOwned
	}
	return nil
}

func (s *Store) requireEstimateGroup(ctx context.Context, userID, id int64) error {
	ok, err := s.rowExists(ctx, "estimate_groups", userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotOwned
	}
	return nil
}

// CreateEstimateSnapshot creates an estimate and copies the user's current
// budget structure into it: every category group except the system Income
// group (identified by containing an is_income category), each group's active
// non-income categories with their assigned amount for month, and the month's
// income rows. The copies carry no references back to the source rows, so the
// real budget stays freely editable afterwards.
func (s *Store) CreateEstimateSnapshot(ctx context.Context, userID int64, name, month string) (int64, error) {
	if name == "" {
		return 0, fmt.Errorf("estimate name required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	estimateID, err := s.txInsertReturningID(ctx, tx,
		`INSERT INTO estimates(user_id, name) VALUES ($1, $2)`, userID, name)
	if err != nil {
		return 0, fmt.Errorf("create estimate: %w", err)
	}

	// Source groups, skipping the system Income group. Copied one by one because
	// each new group id is needed to attach that group's categories.
	rows, err := tx.QueryContext(ctx,
		`SELECT id, name, sort_order FROM category_groups g
		 WHERE g.user_id = $1
		   AND NOT EXISTS (SELECT 1 FROM categories c WHERE c.group_id = g.id AND c.is_income = TRUE)
		 ORDER BY g.sort_order, g.name`, userID)
	if err != nil {
		return 0, fmt.Errorf("snapshot groups: %w", err)
	}
	type srcGroup struct {
		id        int64
		name      string
		sortOrder int64
	}
	var groups []srcGroup
	for rows.Next() {
		var g srcGroup
		if err := rows.Scan(&g.id, &g.name, &g.sortOrder); err != nil {
			_ = rows.Close()
			return 0, err
		}
		groups = append(groups, g)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, g := range groups {
		newGID, err := s.txInsertReturningID(ctx, tx,
			`INSERT INTO estimate_groups(user_id, estimate_id, name, sort_order) VALUES ($1, $2, $3, $4)`,
			userID, estimateID, g.name, g.sortOrder)
		if err != nil {
			return 0, fmt.Errorf("snapshot group %q: %w", g.name, err)
		}
		if _, err := s.txExec(ctx, tx,
			`INSERT INTO estimate_categories(user_id, group_id, name, assigned_cents, sort_order)
			 SELECT c.user_id, $1, c.name, COALESCE(b.assigned_cents, 0), c.sort_order
			 FROM categories c
			 LEFT JOIN budgets b ON b.category_id = c.id AND b.month = $2 AND b.user_id = c.user_id
			 WHERE c.group_id = $3 AND c.user_id = $4 AND c.archived_at IS NULL AND c.is_income = FALSE`,
			newGID, month, g.id, userID); err != nil {
			return 0, fmt.Errorf("snapshot categories of %q: %w", g.name, err)
		}
	}

	if _, err := s.txExec(ctx, tx,
		`INSERT INTO estimate_incomes(user_id, estimate_id, name, amount_cents, sort_order)
		 SELECT user_id, $1, name, amount_cents, sort_order
		 FROM incomes WHERE user_id = $2 AND month = $3`,
		estimateID, userID, month); err != nil {
		return 0, fmt.Errorf("snapshot incomes: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return estimateID, nil
}

// ListEstimates returns the user's estimates, newest first.
func (s *Store) ListEstimates(ctx context.Context, userID int64) ([]Estimate, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, name, created_at, updated_at FROM estimates
		 WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Estimate
	for rows.Next() {
		var e Estimate
		if err := rows.Scan(&e.ID, &e.Name, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEstimate(ctx context.Context, userID, id int64) (Estimate, error) {
	var e Estimate
	err := s.queryOne(ctx,
		`SELECT id, name, created_at, updated_at FROM estimates WHERE id = $1 AND user_id = $2`,
		id, userID).Scan(&e.ID, &e.Name, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// DeleteEstimate removes the estimate; the ON DELETE CASCADE foreign keys take
// its groups, categories, and income rows with it.
func (s *Store) DeleteEstimate(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx, `DELETE FROM estimates WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// --- Groups ---

func (s *Store) CreateEstimateGroup(ctx context.Context, userID, estimateID int64, name string, sortOrder int64) (int64, error) {
	if err := s.requireEstimate(ctx, userID, estimateID); err != nil {
		return 0, err
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO estimate_groups(user_id, estimate_id, name, sort_order) VALUES ($1, $2, $3, $4)`,
		userID, estimateID, name, sortOrder)
	if err != nil {
		return 0, fmt.Errorf("create estimate group: %w", err)
	}
	return id, nil
}

func (s *Store) RenameEstimateGroup(ctx context.Context, userID, id int64, name string) error {
	_, err := s.run(ctx,
		`UPDATE estimate_groups SET name = $1 WHERE id = $2 AND user_id = $3`, name, id, userID)
	return err
}

// DeleteEstimateGroup removes the group and, via cascade, its categories.
// Estimate rows carry no history, so unlike the real budget there is nothing to
// protect — the UI warns before calling this.
func (s *Store) DeleteEstimateGroup(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx, `DELETE FROM estimate_groups WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (s *Store) ListEstimateGroups(ctx context.Context, userID, estimateID int64) ([]EstimateGroup, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, estimate_id, name, sort_order FROM estimate_groups
		 WHERE estimate_id = $1 AND user_id = $2 ORDER BY sort_order, name`, estimateID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EstimateGroup
	for rows.Next() {
		var g EstimateGroup
		if err := rows.Scan(&g.ID, &g.EstimateID, &g.Name, &g.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) MaxEstimateGroupSortOrder(ctx context.Context, userID, estimateID int64) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM estimate_groups WHERE estimate_id = $1 AND user_id = $2`,
		estimateID, userID).Scan(&max)
	return max, err
}

// MinEstimateGroupSortOrder mirrors MinGroupSortOrder: callers subtract 1 to
// prepend a new group at the top, next to the Add group control.
func (s *Store) MinEstimateGroupSortOrder(ctx context.Context, userID, estimateID int64) (int64, error) {
	var min int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MIN(sort_order), 0) FROM estimate_groups WHERE estimate_id = $1 AND user_id = $2`,
		estimateID, userID).Scan(&min)
	return min, err
}

// ReorderEstimateGroups sets each listed group's sort_order to its index in
// ids, in one transaction. Each UPDATE is scoped to the user and estimate, so a
// foreign id is a silent no-op.
func (s *Store) ReorderEstimateGroups(ctx context.Context, userID, estimateID int64, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range ids {
		if _, err := s.txExec(ctx, tx,
			`UPDATE estimate_groups SET sort_order = $1 WHERE id = $2 AND estimate_id = $3 AND user_id = $4`,
			int64(i), id, estimateID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- Categories ---

func (s *Store) CreateEstimateCategory(ctx context.Context, userID, groupID int64, name string, assignedCents, sortOrder int64) (int64, error) {
	if err := s.requireEstimateGroup(ctx, userID, groupID); err != nil {
		return 0, err
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO estimate_categories(user_id, group_id, name, assigned_cents, sort_order) VALUES ($1, $2, $3, $4, $5)`,
		userID, groupID, name, assignedCents, sortOrder)
	if err != nil {
		return 0, fmt.Errorf("create estimate category: %w", err)
	}
	return id, nil
}

func (s *Store) RenameEstimateCategory(ctx context.Context, userID, id int64, name string) error {
	_, err := s.run(ctx,
		`UPDATE estimate_categories SET name = $1 WHERE id = $2 AND user_id = $3`, name, id, userID)
	return err
}

func (s *Store) SetEstimateAssigned(ctx context.Context, userID, id, cents int64) error {
	_, err := s.run(ctx,
		`UPDATE estimate_categories SET assigned_cents = $1 WHERE id = $2 AND user_id = $3`, cents, id, userID)
	return err
}

func (s *Store) DeleteEstimateCategory(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx, `DELETE FROM estimate_categories WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// ListEstimateCategories returns every category in the estimate, ordered for
// grouping under ListEstimateGroups' order.
func (s *Store) ListEstimateCategories(ctx context.Context, userID, estimateID int64) ([]EstimateCategory, error) {
	rows, err := s.queryAll(ctx,
		`SELECT c.id, c.group_id, c.name, c.assigned_cents, c.sort_order
		 FROM estimate_categories c
		 JOIN estimate_groups g ON g.id = c.group_id AND g.user_id = c.user_id
		 WHERE g.estimate_id = $1 AND c.user_id = $2
		 ORDER BY c.sort_order, c.name`, estimateID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EstimateCategory
	for rows.Next() {
		var c EstimateCategory
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Name, &c.AssignedCents, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) MaxEstimateCategorySortOrder(ctx context.Context, userID, groupID int64) (int64, error) {
	var max int64
	err := s.queryOne(ctx,
		`SELECT COALESCE(MAX(sort_order), 0) FROM estimate_categories WHERE group_id = $1 AND user_id = $2`,
		groupID, userID).Scan(&max)
	return max, err
}

// ReorderEstimateCategories persists a group's category order (and, for a
// cross-group drag, the move) by setting each listed category's group_id to
// groupID and its sort_order to its index in catIDs. Mirrors ReorderCategories.
func (s *Store) ReorderEstimateCategories(ctx context.Context, userID, groupID int64, catIDs []int64) error {
	if err := s.requireEstimateGroup(ctx, userID, groupID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range catIDs {
		if _, err := s.txExec(ctx, tx,
			`UPDATE estimate_categories SET group_id = $1, sort_order = $2 WHERE id = $3 AND user_id = $4`,
			groupID, int64(i), id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- Incomes ---

func (s *Store) CreateEstimateIncome(ctx context.Context, userID, estimateID int64, name string, amountCents, sortOrder int64) (int64, error) {
	if err := s.requireEstimate(ctx, userID, estimateID); err != nil {
		return 0, err
	}
	id, err := s.insertReturningID(ctx,
		`INSERT INTO estimate_incomes(user_id, estimate_id, name, amount_cents, sort_order) VALUES ($1, $2, $3, $4, $5)`,
		userID, estimateID, name, amountCents, sortOrder)
	if err != nil {
		return 0, fmt.Errorf("create estimate income: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateEstimateIncome(ctx context.Context, userID int64, in EstimateIncome) error {
	_, err := s.run(ctx,
		`UPDATE estimate_incomes SET name = $1, amount_cents = $2 WHERE id = $3 AND user_id = $4`,
		in.Name, in.AmountCents, in.ID, userID)
	return err
}

func (s *Store) DeleteEstimateIncome(ctx context.Context, userID, id int64) error {
	_, err := s.run(ctx, `DELETE FROM estimate_incomes WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (s *Store) ListEstimateIncomes(ctx context.Context, userID, estimateID int64) ([]EstimateIncome, error) {
	rows, err := s.queryAll(ctx,
		`SELECT id, estimate_id, name, amount_cents, sort_order FROM estimate_incomes
		 WHERE estimate_id = $1 AND user_id = $2 ORDER BY sort_order, id`, estimateID, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EstimateIncome
	for rows.Next() {
		var i EstimateIncome
		if err := rows.Scan(&i.ID, &i.EstimateID, &i.Name, &i.AmountCents, &i.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// GetEstimateIncome returns one income row, for the field-merge update pattern.
func (s *Store) GetEstimateIncome(ctx context.Context, userID, id int64) (EstimateIncome, error) {
	var i EstimateIncome
	err := s.queryOne(ctx,
		`SELECT id, estimate_id, name, amount_cents, sort_order FROM estimate_incomes
		 WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&i.ID, &i.EstimateID, &i.Name, &i.AmountCents, &i.SortOrder)
	return i, err
}

// GetEstimateCategory returns one category row (with its estimate id resolved
// through the group), for handlers that refresh a single group after an edit.
func (s *Store) GetEstimateCategory(ctx context.Context, userID, id int64) (EstimateCategory, error) {
	var c EstimateCategory
	err := s.queryOne(ctx,
		`SELECT id, group_id, name, assigned_cents, sort_order FROM estimate_categories
		 WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&c.ID, &c.GroupID, &c.Name, &c.AssignedCents, &c.SortOrder)
	return c, err
}

// EstimateIDForGroup resolves the estimate a group belongs to, scoped to the
// user. Handlers use it to rebuild page data from a group-level route param.
func (s *Store) EstimateIDForGroup(ctx context.Context, userID, groupID int64) (int64, error) {
	var id int64
	err := s.queryOne(ctx,
		`SELECT estimate_id FROM estimate_groups WHERE id = $1 AND user_id = $2`, groupID, userID).Scan(&id)
	return id, err
}
