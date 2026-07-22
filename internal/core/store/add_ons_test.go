package store

import (
	"context"
	"errors"
	"testing"
)

func TestAddOnsCatalogAndToggle(t *testing.T) {
	s, uid := newTestStoreUser(t)
	ctx := context.Background()

	// The catalog is seeded by migration 00011 with the free paydown add-on, and
	// a fresh user has it disabled (no user_add_ons row).
	addOns, err := s.ListAddOnsForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	paydown, ok := findAddOn(addOns, "paydown")
	if !ok {
		t.Fatal("paydown add-on missing from catalog")
	}
	if paydown.Enabled {
		t.Error("paydown should default to disabled")
	}
	if paydown.PriceCents != 0 {
		t.Errorf("paydown price = %d, want 0 (free)", paydown.PriceCents)
	}
	if slugs, _ := s.EnabledAddOnSlugs(ctx, uid); len(slugs) != 0 {
		t.Errorf("enabled slugs = %v, want none", slugs)
	}

	// Enable it, then toggle it back off — the row is reused, not churned.
	if err := s.SetAddOnEnabled(ctx, uid, "paydown", true); err != nil {
		t.Fatal(err)
	}
	if slugs, _ := s.EnabledAddOnSlugs(ctx, uid); len(slugs) != 1 || slugs[0] != "paydown" {
		t.Errorf("enabled slugs = %v, want [paydown]", slugs)
	}
	if err := s.SetAddOnEnabled(ctx, uid, "paydown", false); err != nil {
		t.Fatal(err)
	}
	if slugs, _ := s.EnabledAddOnSlugs(ctx, uid); len(slugs) != 0 {
		t.Errorf("enabled slugs after disable = %v, want none", slugs)
	}

	// An unknown slug is a not-found error, not a silent no-op.
	if err := s.SetAddOnEnabled(ctx, uid, "nope", true); !errors.Is(err, ErrAddOnNotFound) {
		t.Errorf("SetAddOnEnabled(nope) err = %v, want ErrAddOnNotFound", err)
	}
}

func findAddOn(list []AddOn, slug string) (AddOn, bool) {
	for _, a := range list {
		if a.Slug == slug {
			return a, true
		}
	}
	return AddOn{}, false
}
