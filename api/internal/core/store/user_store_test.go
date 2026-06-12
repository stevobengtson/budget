package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestUserStore_ScopesAccountsByUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const userA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const userB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	idA, err := s.For(userA).CreateAccount(ctx, Account{
		Name: "A-Checking", Type: TypeChecking, StartingBalanceCents: 100,
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	idB, err := s.For(userB).CreateAccount(ctx, Account{
		Name: "B-Savings", Type: TypeSavings, StartingBalanceCents: 200,
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	aAccts, err := s.For(userA).ListAccounts(ctx, true)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(aAccts) != 1 || aAccts[0].ID != idA {
		t.Fatalf("user A should see only its own account; got %d", len(aAccts))
	}

	bAccts, err := s.For(userB).ListAccounts(ctx, true)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(bAccts) != 1 || bAccts[0].ID != idB {
		t.Fatalf("user B should see only its own account; got %d", len(bAccts))
	}

	// A must not be able to read B's account by id.
	if _, err := s.For(userA).GetAccount(ctx, idB); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-user GetAccount: expected sql.ErrNoRows, got %v", err)
	}
}
