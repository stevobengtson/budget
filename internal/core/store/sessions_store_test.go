package store

import (
	"context"
	"testing"
	"time"
)

func makeSession(t *testing.T, s *Store, userID int64, tokenHash string) {
	t.Helper()
	err := s.CreateSession(context.Background(), userID, tokenHash, time.Now().Add(time.Hour),
		SessionInfo{UserAgent: "ua", IP: "127.0.0.1", Client: "web", Label: "Test Browser"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateSessionDefaultsClientToWeb(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "client@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, uid, "h1", time.Now().Add(time.Hour), SessionInfo{}); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSessionByTokenHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Client != "web" {
		t.Fatalf("client = %q, want web", sess.Client)
	}
}

func TestListSessionsForUserReturnsProvenance(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "list@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	makeSession(t, s, uid, "h1")
	makeSession(t, s, uid, "h2")

	sessions, err := s.ListSessionsForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	// The pre-existing user_agent/ip columns were never selected before; make
	// sure the list surface actually carries them.
	if sessions[0].UserAgent != "ua" || sessions[0].IP != "127.0.0.1" || sessions[0].Label != "Test Browser" {
		t.Fatalf("provenance not loaded: %+v", sessions[0])
	}
}

func TestListSessionsForUserExcludesExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "expired@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	makeSession(t, s, uid, "live")
	if err := s.CreateSession(ctx, uid, "dead", time.Now().Add(-time.Hour), SessionInfo{}); err != nil {
		t.Fatal(err)
	}

	sessions, err := s.ListSessionsForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].TokenHash != "live" {
		t.Fatalf("expired sessions must not be listed, got %+v", sessions)
	}
}

// The revoke path is scoped by user_id inside the DELETE itself, so this is a
// structural guarantee rather than a check that could be forgotten at a callsite.
func TestDeleteSessionForUserCannotTouchAnotherUsersSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	alice, err := s.CreateUser(ctx, "alice@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "bob@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	makeSession(t, s, bob, "bobs-session")

	bobs, err := s.ListSessionsForUser(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteSessionForUser(ctx, alice, bobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("alice must not be able to revoke bob's session")
	}
	if _, err := s.GetSessionByTokenHash(ctx, "bobs-session"); err != nil {
		t.Fatal("bob's session should still exist")
	}

	deleted, err = s.DeleteSessionForUser(ctx, bob, bobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("bob should be able to revoke his own session")
	}
}

func TestDeleteSessionsForUserExceptKeepsTheCurrentOne(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "revoke@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, "bystander@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	makeSession(t, s, uid, "current")
	makeSession(t, s, uid, "phone")
	makeSession(t, s, uid, "laptop")
	makeSession(t, s, other, "untouched")

	n, err := s.DeleteSessionsForUserExcept(ctx, uid, "current")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("revoked %d, want 2", n)
	}
	if _, err := s.GetSessionByTokenHash(ctx, "current"); err != nil {
		t.Fatal("the current session must survive")
	}
	if _, err := s.GetSessionByTokenHash(ctx, "untouched"); err != nil {
		t.Fatal("another user's session must be untouched")
	}
}

func TestTouchSessionOnlyWritesWhenStale(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "touch@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	makeSession(t, s, uid, "h1")

	before, err := s.GetSessionByTokenHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}

	// Fresh session, generous staleness window: must not write.
	if err := s.TouchSession(ctx, "h1", time.Hour); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetSessionByTokenHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Fatal("a fresh session must not be rewritten on every request")
	}

	// Zero window makes it stale immediately: must write.
	if err := s.TouchSession(ctx, "h1", 0); err != nil {
		t.Fatal(err)
	}
	after, err = s.GetSessionByTokenHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Fatal("a stale session should have been touched")
	}
}

func TestAuthenticatedAtFallsBackToCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid, err := s.CreateUser(ctx, "stepup@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	makeSession(t, s, uid, "h1")

	sess, err := s.GetSessionByTokenHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ReauthAt != nil {
		t.Fatal("a new session has no separate reauth timestamp")
	}
	if !sess.AuthenticatedAt().Equal(sess.CreatedAt) {
		t.Fatal("AuthenticatedAt should fall back to CreatedAt")
	}

	if err := s.MarkSessionReauthenticated(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	sess, err = s.GetSessionByTokenHash(ctx, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ReauthAt == nil || !sess.AuthenticatedAt().After(sess.CreatedAt) {
		t.Fatalf("step-up should move AuthenticatedAt forward, got %+v", sess.ReauthAt)
	}
}
