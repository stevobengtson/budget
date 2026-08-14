package web

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/sbengtson/budget/internal/core/auth"
	"github.com/sbengtson/budget/internal/core/config"
	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/core/store"
	"github.com/sbengtson/budget/internal/web/handlers"
)

// serveOnboarding is serveAuthed's counterpart for the first-run wizard: the
// seeded user is verified and signed in but deliberately NOT marked onboarded,
// which is the state a real signup is in.
func serveOnboarding(t *testing.T) (*httptest.Server, *http.Client, *store.Store, int64) {
	t.Helper()
	s := store.New(openTestDB(t))
	cfg, err := config.Load(viper.New())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(s, cfg)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx := context.Background()
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.CreateUser(ctx, "new@example.com", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmailVerified(ctx, uid); err != nil {
		t.Fatal(err)
	}
	raw, err := auth.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, uid, auth.HashToken(raw), time.Now().Add(time.Hour), store.SessionInfo{UserAgent: "test", IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(ts.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: handlers.SessionCookieName, Value: raw, Path: "/"}})
	return ts, &http.Client{Jar: jar}, s, uid
}

// wizardPost submits one wizard step and returns the status and body. Unlike
// admin_test.go's postForm it follows redirects and does not assume a 303 — the
// wizard's intermediate steps render the next step inline, and only Finish
// redirects.
func wizardPost(t *testing.T, c *http.Client, url string, form url.Values) (int, string) {
	t.Helper()
	resp, err := c.PostForm(url, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// A user who has not finished the wizard cannot reach the app.
func TestUnonboardedUserIsSentToWizard(t *testing.T) {
	ts, c, _, _ := serveOnboarding(t)

	noRedirect := &http.Client{Jar: c.Jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Get(ts.URL + "/budget")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /budget = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/welcome" {
		t.Fatalf("redirected to %q, want /welcome", loc)
	}
}

// The whole point of putting language first: the starter categories land in the
// language the user chose, not the one their browser happened to ask for.
func TestWizardSeedsCategoriesInChosenLanguage(t *testing.T) {
	ts, c, s, uid := serveOnboarding(t)
	ctx := context.Background()

	// Step 1: choose French. The next step must come back already translated.
	code, body := wizardPost(t, c, ts.URL+"/welcome/language", url.Values{"locale": {"fr-CA"}})
	if code != http.StatusOK {
		t.Fatalf("language step = %d, want 200", code)
	}
	if !strings.Contains(body, "Votre budget de départ") {
		t.Errorf("step 2 did not render in French:\n%s", truncate(body, 400))
	}
	// The French category names must be what step 2 offers for review.
	if !strings.Contains(body, "Épicerie") {
		t.Errorf("step 2 missing French starter category names")
	}

	// Step 2 -> 3, keeping two of the three groups.
	code, body = wizardPost(t, c, ts.URL+"/welcome/categories", url.Values{"groups": {"bills", "savings"}})
	if code != http.StatusOK {
		t.Fatalf("categories step = %d, want 200", code)
	}
	if !strings.Contains(body, "Votre premier compte") {
		t.Errorf("step 3 did not render in French:\n%s", truncate(body, 400))
	}

	// Step 3: finish.
	code, _ = wizardPost(t, c, ts.URL+"/welcome/finish", url.Values{
		"groups": {"bills", "savings"}, "name": {"Compte principal"},
		"type": {"checking"}, "starting_balance": {"1 234,56"},
	})
	if code != http.StatusOK { // followed the redirect to /budget
		t.Fatalf("finish = %d, want 200 after redirect", code)
	}

	// The budget must now exist, in French, with only the kept groups.
	groups, err := s.ListGroups(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range groups {
		names = append(names, g.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"Revenus", "Factures", "Épargne"} {
		if !strings.Contains(joined, want) {
			t.Errorf("groups %q missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "Quotidien") {
		t.Errorf("groups %q contains the group the user dropped", joined)
	}

	cats, err := s.ListCategories(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	var catNames []string
	for _, c := range cats {
		catNames = append(catNames, c.Name)
	}
	catJoined := strings.Join(catNames, ",")
	if !strings.Contains(catJoined, "Loyer/Hypothèque") {
		t.Errorf("categories %q missing the French starter names", catJoined)
	}
	if strings.Contains(catJoined, "Groceries") || strings.Contains(catJoined, "Rent/Mortgage") {
		t.Errorf("categories %q contain English names", catJoined)
	}

	// The account was created, and the fr-CA amount parsed with a comma decimal.
	accts, err := s.ListAccounts(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(accts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(accts))
	}
	if accts[0].Name != "Compte principal" {
		t.Errorf("account name = %q", accts[0].Name)
	}
	if accts[0].StartingBalanceCents != 123456 {
		t.Errorf("starting balance = %d, want 123456", accts[0].StartingBalanceCents)
	}

	// And the wizard is done, so the app is reachable and never re-runs.
	u, err := s.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Onboarded() {
		t.Error("user is not marked onboarded after finishing")
	}
	if u.Locale != i18n.FrCA {
		t.Errorf("locale = %q, want fr-CA", u.Locale)
	}
}

// English is the other half of the same path, and the default group selection
// (nothing unticked) must seed every group.
func TestWizardEnglishSeedsAllGroups(t *testing.T) {
	ts, c, s, uid := serveOnboarding(t)
	ctx := context.Background()

	wizardPost(t, c, ts.URL+"/welcome/language", url.Values{"locale": {"en-CA"}})
	wizardPost(t, c, ts.URL+"/welcome/categories", url.Values{"groups": {"bills", "everyday", "savings"}})
	code, _ := wizardPost(t, c, ts.URL+"/welcome/finish", url.Values{
		"groups": {"bills", "everyday", "savings"}, "name": {"Main Chequing"},
		"type": {"checking"}, "starting_balance": {"$1,234.56"},
	})
	if code != http.StatusOK {
		t.Fatalf("finish = %d", code)
	}

	groups, err := s.ListGroups(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1+len(store.StarterGroups) {
		t.Fatalf("groups = %d, want %d", len(groups), 1+len(store.StarterGroups))
	}
	accts, _ := s.ListAccounts(ctx, uid, false)
	if len(accts) != 1 || accts[0].StartingBalanceCents != 123456 {
		t.Fatalf("account not created correctly: %+v", accts)
	}
}

// A failed last step must not leave a half-built budget or a finished wizard.
func TestWizardFinishRejectsBlankNameWithoutSeeding(t *testing.T) {
	ts, c, s, uid := serveOnboarding(t)
	ctx := context.Background()

	wizardPost(t, c, ts.URL+"/welcome/language", url.Values{"locale": {"en-CA"}})
	code, _ := wizardPost(t, c, ts.URL+"/welcome/finish", url.Values{
		"groups": {"bills"}, "name": {"  "}, "type": {"checking"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("finish with blank name = %d, want 400", code)
	}

	cats, _ := s.ListCategories(ctx, uid, false)
	if len(cats) != 0 {
		t.Errorf("categories = %d, want 0 — nothing should be seeded on a failed finish", len(cats))
	}
	u, _ := s.GetUserByID(ctx, uid)
	if u.Onboarded() {
		t.Error("user marked onboarded despite the finish failing")
	}
}

// Dropping every group is a real answer, not an unanswered step: the user gets
// the Income category and nothing else.
func TestWizardAllGroupsDroppedSeedsOnlyIncome(t *testing.T) {
	ts, c, s, uid := serveOnboarding(t)
	ctx := context.Background()

	wizardPost(t, c, ts.URL+"/welcome/language", url.Values{"locale": {"en-CA"}})
	wizardPost(t, c, ts.URL+"/welcome/finish", url.Values{
		"name": {"Main Chequing"}, "type": {"checking"},
	})

	cats, err := s.ListCategories(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 1 || !cats[0].IsIncome {
		t.Fatalf("categories = %+v, want only the Income category", cats)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
