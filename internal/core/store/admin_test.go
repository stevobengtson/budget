package store

import (
	"context"
	"testing"
	"time"
)

// TestCompGrantRevokeAndExpiry pins the rule that IsBillingExempt combines the
// flag with the expiry: a dated comp grants access until it lapses, a lifetime
// comp has no end, and revoking clears both.
func TestCompGrantRevokeAndExpiry(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	if exempt, err := s.IsBillingExempt(ctx, uid); err != nil || exempt {
		t.Fatalf("fresh user exempt = %v err %v, want false", exempt, err)
	}

	// A dated comp in the future grants access.
	future := time.Now().Add(30 * 24 * time.Hour)
	if err := s.GrantComp(ctx, uid, &future); err != nil {
		t.Fatal(err)
	}
	if exempt, _ := s.IsBillingExempt(ctx, uid); !exempt {
		t.Error("comp until +30d: exempt = false, want true")
	}
	flagSet, until, err := s.GetComp(ctx, uid)
	if err != nil || !flagSet || until == nil {
		t.Fatalf("GetComp = (%v, %v, %v), want set with an end date", flagSet, until, err)
	}

	// A comp whose end date has passed does not, even though the flag is still
	// set — this is the case a bare billing_exempt lookup would get wrong.
	past := time.Now().Add(-time.Hour)
	if err := s.GrantComp(ctx, uid, &past); err != nil {
		t.Fatal(err)
	}
	if exempt, _ := s.IsBillingExempt(ctx, uid); exempt {
		t.Error("comp until -1h: exempt = true, want false")
	}
	if flagSet, _, _ := s.GetComp(ctx, uid); !flagSet {
		t.Error("expired comp: flag cleared, want it still stored")
	}

	// A lifetime comp is a NULL end date, not a far-future sentinel.
	if err := s.GrantComp(ctx, uid, nil); err != nil {
		t.Fatal(err)
	}
	if exempt, _ := s.IsBillingExempt(ctx, uid); !exempt {
		t.Error("lifetime comp: exempt = false, want true")
	}
	if _, until, _ := s.GetComp(ctx, uid); until != nil {
		t.Errorf("lifetime comp until = %v, want nil", until)
	}

	if err := s.RevokeComp(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if exempt, _ := s.IsBillingExempt(ctx, uid); exempt {
		t.Error("after revoke: exempt = true, want false")
	}
	if flagSet, until, _ := s.GetComp(ctx, uid); flagSet || until != nil {
		t.Errorf("after revoke: flag=%v until=%v, want cleared", flagSet, until)
	}
}

// TestSetUserDisabledRevokesSessions checks the suspension is immediate: the
// user's sessions must be gone, not merely rejected at next lookup.
func TestSetUserDisabledRevokesSessions(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	exp := time.Now().Add(time.Hour)
	if err := s.CreateSession(ctx, uid, "hash-abc", exp, SessionInfo{UserAgent: "test-agent", IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSessionByTokenHash(ctx, "hash-abc"); err != nil {
		t.Fatalf("session should exist before disable: %v", err)
	}

	if err := s.SetUserDisabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSessionByTokenHash(ctx, "hash-abc"); err == nil {
		t.Error("session survived disable, want it deleted")
	}
	u, err := s.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Disabled() {
		t.Error("user.Disabled() = false after disable")
	}

	if err := s.SetUserDisabled(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetUserByID(ctx, uid); u.Disabled() {
		t.Error("user.Disabled() = true after re-enable")
	}
}

// TestHasAccessGrantingSubscription covers the predicate that gates admin
// deletion: only a live subscription blocks it.
func TestHasAccessGrantingSubscription(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	if live, err := s.HasAccessGrantingSubscription(ctx, uid); err != nil || live {
		t.Fatalf("never subscribed: live = %v err %v, want false", live, err)
	}

	sub := Subscription{
		UserID: uid, StripeSubscriptionID: "sub_admin_1", StripeCustomerID: "cus_admin",
		PriceID: "price_base", Status: "canceled", Currency: "usd",
	}
	if err := s.UpsertSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.HasAccessGrantingSubscription(ctx, uid); live {
		t.Error("canceled subscription: live = true, want false")
	}

	sub.Status = "past_due"
	if err := s.UpsertSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.HasAccessGrantingSubscription(ctx, uid); !live {
		t.Error("past_due subscription: live = false, want true (Stripe is still dunning)")
	}
}

// TestListUsersPagination checks the page window and the derived columns the
// admin table renders.
func TestListUsersPagination(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if _, err := s.CreateUser(ctx, pageEmail(i), "hash"); err != nil {
			t.Fatal(err)
		}
	}
	total, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("CountUsers = %d, want 5", total)
	}

	first, err := s.ListUsers(ctx, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("page 1 length = %d, want 2", len(first))
	}
	second, err := s.ListUsers(ctx, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("page 2 length = %d, want 2", len(second))
	}
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Errorf("user %d appears on both pages", a.ID)
			}
		}
	}
	// Past the end is empty, not an error — the handler clamps, but the store
	// must not blow up if it ever does not.
	if rows, err := s.ListUsers(ctx, 2, 99); err != nil || len(rows) != 0 {
		t.Errorf("offset past end = %d rows err %v, want 0", len(rows), err)
	}

	// Derived columns: subscription status, comp, disabled.
	if err := s.UpsertSubscription(ctx, Subscription{
		UserID: uid, StripeSubscriptionID: "sub_list", StripeCustomerID: "cus_list",
		PriceID: "price_base", Status: "active", Currency: "usd",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.GrantComp(ctx, uid, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserDisabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListUsers(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got *AdminUserRow
	for i := range rows {
		if rows[i].ID == uid {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatal("owner user missing from list")
	}
	if got.SubStatus != "active" {
		t.Errorf("SubStatus = %q, want active", got.SubStatus)
	}
	if !got.CompActive || got.CompUntil != nil {
		t.Errorf("comp = (%v, %v), want active lifetime", got.CompActive, got.CompUntil)
	}
	if got.DisabledAt == nil {
		t.Error("DisabledAt = nil, want set")
	}
}

func pageEmail(i int) string {
	return string(rune('a'+i)) + "@example.com"
}

// TestSignupSeries checks the series is zero-filled to a fixed length, ends on
// today's bucket, and counts a new signup into it.
func TestSignupSeries(t *testing.T) {
	s, _ := newTestStoreUser(t) // one user created now
	ctx := context.Background()

	daily, err := s.SignupSeries(ctx, SignupDaily, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 7 {
		t.Fatalf("daily series length = %d, want 7", len(daily))
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !daily[6].Start.Equal(today) {
		t.Errorf("last bucket = %v, want today %v", daily[6].Start, today)
	}
	if daily[6].Count != 1 {
		t.Errorf("today's count = %d, want 1", daily[6].Count)
	}
	for i, b := range daily[:6] {
		if b.Count != 0 {
			t.Errorf("bucket %d count = %d, want 0", i, b.Count)
		}
		if !b.Start.Before(daily[i+1].Start) {
			t.Errorf("bucket %d start %v not before next %v", i, b.Start, daily[i+1].Start)
		}
	}

	monthly, err := s.SignupSeries(ctx, SignupMonthly, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(monthly) != 12 {
		t.Fatalf("monthly series length = %d, want 12", len(monthly))
	}
	now := time.Now().UTC()
	wantMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if !monthly[11].Start.Equal(wantMonth) {
		t.Errorf("last monthly bucket = %v, want %v", monthly[11].Start, wantMonth)
	}
	if monthly[11].Count != 1 {
		t.Errorf("this month's count = %d, want 1", monthly[11].Count)
	}
}

// TestCountAdminStats checks the counters, including that the "active
// subscriptions" figure follows AccessGranting rather than a hardcoded list.
func TestCountAdminStats(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	st, err := s.CountAdminStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalUsers != 1 || st.ActiveSubs != 0 || st.Comped != 0 || st.Disabled != 0 {
		t.Fatalf("fresh stats = %+v, want 1 user and nothing else", st)
	}

	if err := s.UpsertSubscription(ctx, Subscription{
		UserID: uid, StripeSubscriptionID: "sub_stats", StripeCustomerID: "cus_stats",
		PriceID: "price_base", Status: "trialing", Currency: "usd",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.GrantComp(ctx, uid, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserDisabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}

	st, err = s.CountAdminStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.ActiveSubs != 1 || st.Comped != 1 || st.Disabled != 1 {
		t.Errorf("stats = %+v, want 1 active sub / 1 comped / 1 disabled", st)
	}
}

// TestSetUserAdmin round-trips the flag that gates the console.
func TestSetUserAdmin(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	if u, _ := s.GetUserByID(ctx, uid); u.IsAdmin {
		t.Fatal("new user is admin, want false")
	}
	if err := s.SetUserAdmin(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetUserByID(ctx, uid); !u.IsAdmin {
		t.Error("IsAdmin = false after granting")
	}
	if err := s.SetUserAdmin(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetUserByID(ctx, uid); u.IsAdmin {
		t.Error("IsAdmin = true after revoking")
	}
}
