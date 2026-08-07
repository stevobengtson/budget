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

// The assertions name the semantic token, not the colour it currently resolves
// to, so a future palette change is a theme.css edit rather than a test edit.

func TestMoneyPositiveUsesPositiveToken(t *testing.T) {
	out := render(t, 12345)
	if !strings.Contains(out, "text-money-positive") {
		t.Errorf("positive should use the positive token, got %q", out)
	}
	if !strings.Contains(out, "$123.45") {
		t.Errorf("missing formatted amount, got %q", out)
	}
}

func TestMoneyNegativeUsesNegativeToken(t *testing.T) {
	out := render(t, -12345)
	if !strings.Contains(out, "text-money-negative") {
		t.Errorf("negative should use the negative token, got %q", out)
	}
	if !strings.Contains(out, "-$123.45") {
		t.Errorf("missing formatted amount, got %q", out)
	}
}

func TestMoneyZeroUsesZeroToken(t *testing.T) {
	out := render(t, 0)
	if !strings.Contains(out, "text-money-zero") {
		t.Errorf("zero should use the zero token, got %q", out)
	}
	if !strings.Contains(out, "$0.00") {
		t.Errorf("missing formatted amount, got %q", out)
	}
}

// Every money figure is tabular so columns of numbers align.
func TestMoneyIsAlwaysTabular(t *testing.T) {
	for _, cents := range []int64{12345, -12345, 0} {
		if out := render(t, cents); !strings.Contains(out, "tabular-nums") {
			t.Errorf("%d should render tabular, got %q", cents, out)
		}
	}
}
