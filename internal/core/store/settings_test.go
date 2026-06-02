package store

import (
	"context"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.GetSetting(ctx, "missing.key"); err != nil {
		t.Fatalf("get missing: %v", err)
	} else if ok {
		t.Fatalf("expected ok=false for missing key")
	}

	if err := s.SetSetting(ctx, "defaults.account_id", "7"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok, err := s.GetSetting(ctx, "defaults.account_id")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || v != "7" {
		t.Fatalf("got (%q,%v), want (\"7\", true)", v, ok)
	}

	// upsert replaces existing value
	if err := s.SetSetting(ctx, "defaults.account_id", "9"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	v, _, _ = s.GetSetting(ctx, "defaults.account_id")
	if v != "9" {
		t.Fatalf("after upsert got %q, want \"9\"", v)
	}

	if err := s.DeleteSetting(ctx, "defaults.account_id"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := s.GetSetting(ctx, "defaults.account_id"); ok {
		t.Fatalf("expected ok=false after delete")
	}
}
