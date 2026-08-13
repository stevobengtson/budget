package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// seedBudgetForSnapshot builds a small real budget: one group with two active
// categories (one assigned), one archived category, and two income rows for the
// month. Returns the group id and the month key used.
func seedBudgetForSnapshot(t *testing.T, s *Store, uid int64) (int64, string) {
	t.Helper()
	ctx := context.Background()
	month := MonthKey(time.Now())

	gid, err := s.CreateGroup(ctx, uid, "Bills", 1)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	rentID, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Rent", SortOrder: 0})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if _, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Hydro", SortOrder: 1}); err != nil {
		t.Fatalf("create category: %v", err)
	}
	oldID, err := s.CreateCategory(ctx, uid, Category{GroupID: gid, Name: "Old", SortOrder: 2})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := s.ArchiveCategory(ctx, uid, oldID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := s.SetAssigned(ctx, uid, month, rentID, 120_000); err != nil {
		t.Fatalf("assign: %v", err)
	}
	for i, name := range []string{"Salary", "Side gig"} {
		if _, err := s.CreateIncome(ctx, uid, Income{Month: month, Name: name, AmountCents: int64(100_000 * (i + 1)), SortOrder: int64(i)}); err != nil {
			t.Fatalf("create income: %v", err)
		}
	}
	return gid, month
}

func TestEstimateSnapshot(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	_, month := seedBudgetForSnapshot(t, s, uid)

	eid, err := s.CreateEstimateSnapshot(ctx, uid, "Aug-2026", month)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	list, err := s.ListEstimates(ctx, uid)
	if err != nil || len(list) != 1 || list[0].Name != "Aug-2026" {
		t.Fatalf("list = %v, %v; want one estimate named Aug-2026", list, err)
	}

	// The system Income group must not be copied — only Bills.
	groups, err := s.ListEstimateGroups(ctx, uid, eid)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "Bills" {
		t.Fatalf("groups = %v, want only Bills", groups)
	}

	// Archived category excluded; assigned value carried over.
	cats, err := s.ListEstimateCategories(ctx, uid, eid)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("categories = %v, want 2 (archived excluded)", cats)
	}
	byName := map[string]EstimateCategory{}
	for _, c := range cats {
		byName[c.Name] = c
	}
	if byName["Rent"].AssignedCents != 120_000 {
		t.Errorf("Rent assigned = %d, want 120000", byName["Rent"].AssignedCents)
	}
	if byName["Hydro"].AssignedCents != 0 {
		t.Errorf("Hydro assigned = %d, want 0", byName["Hydro"].AssignedCents)
	}
	// The snapshot freezes the reference value alongside the editable one.
	if byName["Rent"].InitialAssignedCents != 120_000 {
		t.Errorf("Rent initial = %d, want 120000", byName["Rent"].InitialAssignedCents)
	}

	// Editing the assigned value must not move the frozen initial value, and a
	// category created after the snapshot has no initial value.
	if err := s.SetEstimateAssigned(ctx, uid, byName["Rent"].ID, 99_000); err != nil {
		t.Fatalf("set assigned: %v", err)
	}
	if c, _ := s.GetEstimateCategory(ctx, uid, byName["Rent"].ID); c.AssignedCents != 99_000 || c.InitialAssignedCents != 120_000 {
		t.Errorf("after edit: assigned = %d initial = %d, want 99000 / 120000", c.AssignedCents, c.InitialAssignedCents)
	}
	newID, err := s.CreateEstimateCategory(ctx, uid, groups[0].ID, "New thing", 5_000, 99)
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if c, _ := s.GetEstimateCategory(ctx, uid, newID); c.InitialAssignedCents != 0 {
		t.Errorf("new category initial = %d, want 0", c.InitialAssignedCents)
	}

	incs, err := s.ListEstimateIncomes(ctx, uid, eid)
	if err != nil {
		t.Fatalf("list incomes: %v", err)
	}
	if len(incs) != 2 || incs[0].Name != "Salary" || incs[1].AmountCents != 200_000 {
		t.Fatalf("incomes = %v, want Salary then Side gig 200000", incs)
	}
}

func TestEstimateEditsDoNotTouchRealBudget(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	gid, month := seedBudgetForSnapshot(t, s, uid)

	eid, err := s.CreateEstimateSnapshot(ctx, uid, "plan", month)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	cats, _ := s.ListEstimateCategories(ctx, uid, eid)
	groups, _ := s.ListEstimateGroups(ctx, uid, eid)

	if err := s.RenameEstimateCategory(ctx, uid, cats[0].ID, "Changed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.SetEstimateAssigned(ctx, uid, cats[0].ID, 1); err != nil {
		t.Fatalf("set assigned: %v", err)
	}
	if err := s.DeleteEstimateGroup(ctx, uid, groups[0].ID); err != nil {
		t.Fatalf("delete estimate group: %v", err)
	}

	// The real budget is untouched: group and categories still there, the
	// month's assignment unchanged.
	realCats, err := s.ListCategories(ctx, uid, false)
	if err != nil {
		t.Fatalf("real categories: %v", err)
	}
	var rentID int64
	for _, c := range realCats {
		if c.Name == "Rent" {
			rentID = c.ID
		}
		if c.Name == "Changed" {
			t.Errorf("estimate rename leaked into real categories")
		}
	}
	if rentID == 0 {
		t.Fatalf("real Rent category missing after estimate edits")
	}
	if got, _ := s.GetAssigned(ctx, uid, month, rentID); got != 120_000 {
		t.Errorf("real assigned = %d, want 120000", got)
	}
	realGroups, _ := s.ListGroups(ctx, uid)
	found := false
	for _, g := range realGroups {
		if g.ID == gid {
			found = true
		}
	}
	if !found {
		t.Errorf("real group deleted by estimate group delete")
	}
}

func TestEstimateGroupCategoryCRUDAndReorder(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	_, month := seedBudgetForSnapshot(t, s, uid)
	eid, err := s.CreateEstimateSnapshot(ctx, uid, "plan", month)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	g1 := mustListEstimateGroups(t, s, uid, eid)[0].ID
	g2, err := s.CreateEstimateGroup(ctx, uid, eid, "Fun", 5)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := s.RenameEstimateGroup(ctx, uid, g2, "Fun Money"); err != nil {
		t.Fatalf("rename group: %v", err)
	}
	cid, err := s.CreateEstimateCategory(ctx, uid, g2, "Games", 5_000, 0)
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	// Reorder groups: Fun Money first.
	if err := s.ReorderEstimateGroups(ctx, uid, eid, []int64{g2, g1}); err != nil {
		t.Fatalf("reorder groups: %v", err)
	}
	groups := mustListEstimateGroups(t, s, uid, eid)
	if groups[0].ID != g2 || groups[0].Name != "Fun Money" {
		t.Fatalf("groups after reorder = %v, want Fun Money first", groups)
	}

	// Cross-group move: Games into the Bills group.
	if err := s.ReorderEstimateCategories(ctx, uid, g1, []int64{cid}); err != nil {
		t.Fatalf("reorder categories: %v", err)
	}
	if c, _ := s.GetEstimateCategory(ctx, uid, cid); c.GroupID != g1 {
		t.Errorf("category group = %d, want %d", c.GroupID, g1)
	}

	if err := s.DeleteEstimateCategory(ctx, uid, cid); err != nil {
		t.Fatalf("delete category: %v", err)
	}
	if _, err := s.GetEstimateCategory(ctx, uid, cid); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("deleted category still readable: %v", err)
	}
}

func TestEstimateIncomeCRUD(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	eid, err := s.CreateEstimateSnapshot(ctx, uid, "plan", MonthKey(time.Now()))
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	id, err := s.CreateEstimateIncome(ctx, uid, eid, "Salary", 300_000, 1)
	if err != nil {
		t.Fatalf("create income: %v", err)
	}
	if err := s.UpdateEstimateIncome(ctx, uid, EstimateIncome{ID: id, Name: "Salary (net)", AmountCents: 280_000}); err != nil {
		t.Fatalf("update income: %v", err)
	}
	got, err := s.GetEstimateIncome(ctx, uid, id)
	if err != nil || got.Name != "Salary (net)" || got.AmountCents != 280_000 {
		t.Fatalf("income = %v, %v", got, err)
	}
	if err := s.DeleteEstimateIncome(ctx, uid, id); err != nil {
		t.Fatalf("delete income: %v", err)
	}
	if incs, _ := s.ListEstimateIncomes(ctx, uid, eid); len(incs) != 0 {
		t.Errorf("incomes after delete = %v, want none", incs)
	}
}

func TestEstimateCrossUserScoping(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	_, month := seedBudgetForSnapshot(t, s, uid)
	eid, err := s.CreateEstimateSnapshot(ctx, uid, "mine", month)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	cats, _ := s.ListEstimateCategories(ctx, uid, eid)

	other, err := s.CreateUser(ctx, "intruder@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Creating under someone else's estimate is refused outright.
	if _, err := s.CreateEstimateGroup(ctx, other, eid, "Sneak", 0); !errors.Is(err, ErrNotOwned) {
		t.Errorf("cross-user group create err = %v, want ErrNotOwned", err)
	}
	// Scoped UPDATE/DELETE against another user's rows are silent no-ops.
	if err := s.SetEstimateAssigned(ctx, other, cats[0].ID, 1); err != nil {
		t.Fatalf("cross-user assign: %v", err)
	}
	if c, _ := s.GetEstimateCategory(ctx, uid, cats[0].ID); c.AssignedCents == 1 {
		t.Errorf("cross-user assign mutated the row")
	}
	if err := s.DeleteEstimate(ctx, other, eid); err != nil {
		t.Fatalf("cross-user delete: %v", err)
	}
	if _, err := s.GetEstimate(ctx, uid, eid); err != nil {
		t.Errorf("cross-user delete removed the estimate: %v", err)
	}
}

func TestDeleteEstimateCascades(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()
	_, month := seedBudgetForSnapshot(t, s, uid)
	eid, err := s.CreateEstimateSnapshot(ctx, uid, "gone", month)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := s.DeleteEstimate(ctx, uid, eid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM estimate_groups WHERE estimate_id=$1`,
		`SELECT COUNT(*) FROM estimate_incomes WHERE estimate_id=$1`,
	} {
		var n int
		if err := s.queryOne(ctx, q, eid).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Errorf("%s = %d, want 0 (cascade)", q, n)
		}
	}
}

func mustListEstimateGroups(t *testing.T, s *Store, uid, eid int64) []EstimateGroup {
	t.Helper()
	groups, err := s.ListEstimateGroups(context.Background(), uid, eid)
	if err != nil {
		t.Fatalf("list estimate groups: %v", err)
	}
	return groups
}
