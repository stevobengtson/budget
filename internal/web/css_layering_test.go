package web

import (
	"os"
	"strings"
	"testing"
)

// TestStateRulesAreUnlayered guards a failure mode that no rendered-HTML test
// can see: CSS cascade layers outrank specificity, so a rule inside
// @layer base loses to any Tailwind utility on the same element no matter how
// specific its selector is.
//
// That is exactly how group collapse broke. The rule
//
//	#budget-table .group-collapsed > *:not([data-group-head]) { display: none }
//
// worked while category rows were <tr> elements carrying no display utility.
// When the rows became a div grid, the `grid` utility in @layer utilities began
// winning, and collapsing a group silently stopped hiding anything — correct
// markup, correct selector, no error anywhere.
//
// These rules decide whether an element is visible and must beat the utilities
// on it, so they live unlayered, where they outrank every layer.
func TestStateRulesAreUnlayered(t *testing.T) {
	src, err := os.ReadFile("tailwind/input.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)

	base, ok := layerBlock(css, "base")
	if !ok {
		t.Fatal("no @layer base block found in input.css")
	}
	for _, sel := range []string{
		".group-collapsed",      // group collapse
		".js-group-caret",       // its disclosure caret
		".tx-transfer-only",     // transaction type field swapping
		".tx-plain-only",        //
		".tx-transfer-category", //
	} {
		if strings.Contains(base, sel) {
			t.Errorf("%s is inside @layer base; a utility class will override it. "+
				"Move it to the unlayered block at the end of input.css.", sel)
		}
		if !strings.Contains(css, sel) {
			t.Errorf("%s not found in input.css at all", sel)
		}
	}
}

// layerBlock returns the body of the named @layer, matched by braces so nested
// blocks inside it don't end the search early.
func layerBlock(css, name string) (string, bool) {
	i := strings.Index(css, "@layer "+name+" {")
	if i < 0 {
		return "", false
	}
	depth, start := 0, i
	for j := i; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start : j+1], true
			}
		}
	}
	return "", false
}
