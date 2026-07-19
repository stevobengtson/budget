package signedmoney

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func render(t *testing.T, cents int64) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Money(cents).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestMoneyPositiveIsEmerald(t *testing.T) {
	out := render(t, 12345)
	if !strings.Contains(out, "text-emerald-600") {
		t.Errorf("positive should be emerald, got %q", out)
	}
	if !strings.Contains(out, "$123.45") {
		t.Errorf("missing formatted amount, got %q", out)
	}
}

func TestMoneyNegativeIsRed(t *testing.T) {
	out := render(t, -12345)
	if !strings.Contains(out, "text-destructive") {
		t.Errorf("negative should be red, got %q", out)
	}
	if !strings.Contains(out, "-$123.45") {
		t.Errorf("missing formatted amount, got %q", out)
	}
}

func TestMoneyZeroIsMuted(t *testing.T) {
	out := render(t, 0)
	if !strings.Contains(out, "text-muted-foreground") {
		t.Errorf("zero should be muted, got %q", out)
	}
	if !strings.Contains(out, "$0.00") {
		t.Errorf("missing formatted amount, got %q", out)
	}
}
