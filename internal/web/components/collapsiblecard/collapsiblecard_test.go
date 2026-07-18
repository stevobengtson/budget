package collapsiblecard

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func render(t *testing.T, p Props) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Card(p).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRendersTitleAndPersistKey(t *testing.T) {
	out := render(t, Props{Title: "Income", ID: "income-collapsible", PersistKey: "income", Open: true})
	if !strings.Contains(out, "Income") {
		t.Errorf("missing title, got %q", out)
	}
	if !strings.Contains(out, `data-persist-collapsible="income"`) {
		t.Errorf("missing persist attr, got %q", out)
	}
	if !strings.Contains(out, "income-collapsible") {
		t.Errorf("missing id, got %q", out)
	}
}

func TestOpenAppliesOpenClass(t *testing.T) {
	// templui's collapsible.Content base classes always embed the literal
	// substring "tui-collapsible-open" as part of the arbitrary-variant
	// selector "[&.tui-collapsible-open]:grid-rows-[1fr]", independent of the
	// Open prop. A bare strings.Contains can't distinguish open from closed,
	// so check for the class actually being applied (i.e. present at the
	// front of the content div's class attribute).
	const appliedClass = `class="tui-collapsible-open `

	open := render(t, Props{Title: "X", ID: "x", PersistKey: "x", Open: true})
	if !strings.Contains(open, appliedClass) {
		t.Errorf("open card should apply tui-collapsible-open class, got %q", open)
	}
	closed := render(t, Props{Title: "X", ID: "x", PersistKey: "x", Open: false})
	if strings.Contains(closed, appliedClass) {
		t.Errorf("closed card should not apply tui-collapsible-open class, got %q", closed)
	}
}
