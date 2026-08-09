package web

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/sbengtson/budget/internal/core/store"
)

// archiveFixture builds a budget with one category holding $124.50 and returns
// its id. An account is required or /budget renders the first-run screen.
func archiveFixture(t *testing.T, s *store.Store, uid int64) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateAccount(ctx, uid, store.Account{Name: "Checking", Type: "checking"}); err != nil {
		t.Fatal(err)
	}
	gid, err := s.CreateGroup(ctx, uid, "Monthly", 1)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := s.CreateCategory(ctx, uid, store.Category{GroupID: gid, Name: "Vacation"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssigned(ctx, uid, "2026-07", cat, 12_450); err != nil {
		t.Fatal(err)
	}
	return cat
}

// The archive prompt must name the amount at stake. A category carrying a
// balance and one carrying nothing are materially different decisions, and the
// generic "Archive this category?" told the user neither.
func TestArchiveConfirmNamesTheBalance(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cat := archiveFixture(t, s, uid)

	resp, err := client.Get(ts.URL + "/budget?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)

	if !strings.Contains(body, "$124.50") {
		t.Error("archive confirm should state the balance the category holds")
	}
	if !strings.Contains(body, "Still in envelopes") {
		t.Error("archive confirm should name the total the balance stops counting toward")
	}
	if strings.Contains(body, "Archive this category?") {
		t.Error("the old balance-blind confirm text is still being rendered")
	}

	// A category with nothing in it gets the short form, with no dollar figure
	// invented for it.
	if _, err := s.CreateCategory(context.Background(), uid, store.Category{
		GroupID: mustGroupID(t, s, uid, cat), Name: "Empty",
	}); err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Get(ts.URL + "/budget?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAll(t, resp2), "Archive Empty?") {
		t.Error("a zero-balance category should still get a named confirm")
	}
}

// The Archived control is the only route back from archiving, so it must appear
// exactly when there is something to restore — and not before.
func TestArchivedControlAppearsOnlyWhenNeeded(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	cat := archiveFixture(t, s, uid)

	resp, err := client.Get(ts.URL + "/budget?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readAll(t, resp), "/budget/archived/panel") {
		t.Error("Archived control should not render with nothing archived")
	}

	if err := s.ArchiveCategory(context.Background(), uid, cat); err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Get(ts.URL + "/budget?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAll(t, resp2), "/budget/archived/panel") {
		t.Error("Archived control should render once a category is archived")
	}
}

// The panel shows what is parked and what it holds; unarchiving from it restores
// the row and refreshes the budget region behind the modal out of band.
func TestArchivedPanelAndUnarchive(t *testing.T) {
	s := store.New(openTestDB(t))
	ts, client, uid := serveAuthed(t, s)
	ctx := context.Background()
	cat := archiveFixture(t, s, uid)
	if err := s.ArchiveCategory(ctx, uid, cat); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get(ts.URL + "/budget/archived/panel?month=2026-07")
	if err != nil {
		t.Fatal(err)
	}
	panel := readAll(t, resp)
	for _, want := range []string{"Archived categories", "Vacation", "Monthly", "$124.50", "Unarchive"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel missing %q", want)
		}
	}

	unarchiveURL := ts.URL + "/budget/category/" + strconv.FormatInt(cat, 10) + "/unarchive?month=2026-07"
	resp2, err := client.PostForm(unarchiveURL, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp2)

	if !strings.Contains(body, "Nothing archived") {
		t.Error("the panel list should re-render empty after the last unarchive")
	}
	// The out-of-band region carries the restored row and its money back to the
	// page behind the modal.
	if !strings.Contains(body, `hx-swap-oob="true"`) {
		t.Error("unarchive should refresh the budget region out of band")
	}
	if !strings.Contains(body, "Vacation") || !strings.Contains(body, "$124.50") {
		t.Error("restored category and its balance should be back in the budget region")
	}

	cats, _ := s.ListCategories(ctx, uid, false)
	found := false
	for _, c := range cats {
		if c.ID == cat {
			found = true
		}
	}
	if !found {
		t.Error("unarchived category should be active again")
	}
}

func mustGroupID(t *testing.T, s *store.Store, uid, catID int64) int64 {
	t.Helper()
	cats, err := s.ListCategories(context.Background(), uid, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.ID == catID {
			return c.GroupID
		}
	}
	t.Fatalf("category %d not found", catID)
	return 0
}
