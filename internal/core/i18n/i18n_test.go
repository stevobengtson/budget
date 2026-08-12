package i18n

import (
	"context"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in     string
		want   Locale
		wantOK bool
	}{
		{"en-CA", EnCA, true},
		{"EN-ca", EnCA, true}, // tags are case-insensitive
		{"fr-CA", FrCA, true},
		{"fr", FrCA, true},    // base-language match
		{"fr-FR", FrCA, true}, // we ship one French; serve it
		{"en-US", EnCA, true},
		{"de", Default, false},
		{"", Default, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("Parse(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestFromAcceptLanguage(t *testing.T) {
	cases := []struct {
		header string
		want   Locale
		wantOK bool
	}{
		{"fr-CA,fr;q=0.9,en;q=0.8", FrCA, true},
		{"en-US,en;q=0.9", EnCA, true},
		// The first *supported* tag wins, not the first tag: a browser asking for
		// German ahead of French should still get French, not English.
		{"de-DE,de;q=0.9,fr;q=0.8", FrCA, true},
		{"de,ja", Default, false},
		{"", Default, false},
		{"*", Default, false},
	}
	for _, c := range cases {
		got, ok := FromAcceptLanguage(c.header)
		if got != c.want || ok != c.wantOK {
			t.Errorf("FromAcceptLanguage(%q) = %q,%v; want %q,%v", c.header, got, ok, c.want, c.wantOK)
		}
	}
}

func TestTranslate(t *testing.T) {
	en := WithLocale(context.Background(), EnCA)
	fr := WithLocale(context.Background(), FrCA)

	if got := T(en, "menu.log_out"); got != "Log out" {
		t.Errorf("en menu.log_out = %q", got)
	}
	if got := T(fr, "menu.log_out"); got != "Déconnexion" {
		t.Errorf("fr menu.log_out = %q", got)
	}
}

// A context that never saw the middleware (a background job, a bare test) must
// still translate rather than panic or render an ID.
func TestTranslateWithoutLocaleUsesDefault(t *testing.T) {
	if got := T(context.Background(), "menu.settings"); got != "Settings" {
		t.Errorf("bare context = %q, want the en-CA string", got)
	}
}

// A missing key renders as the key. Loud in a screenshot, harmless to the page.
func TestMissingKeyRendersID(t *testing.T) {
	ctx := WithLocale(context.Background(), EnCA)
	if got := T(ctx, "does.not.exist"); got != "does.not.exist" {
		t.Errorf("missing key = %q, want the id", got)
	}
}

// Every key in the source catalog must exist in every translation. Without this
// the fallback silently serves English inside a French page and nobody notices.
func TestTranslationsCoverSourceCatalog(t *testing.T) {
	source := catalogKeys(t, Default)
	if len(source) == 0 {
		t.Fatalf("source catalog %s is empty", Default)
	}
	for _, l := range Supported {
		if l == Default {
			continue
		}
		have := catalogKeys(t, l)
		for id := range source {
			if !have[id] {
				t.Errorf("%s is missing message %q", l, id)
			}
		}
		for id := range have {
			if !source[id] {
				t.Errorf("%s defines %q, which is not in the source catalog %s", l, id, Default)
			}
		}
	}
}

// catalogKeys reads a catalog straight off disk and returns its flattened
// dotted message IDs. It deliberately does not go through the bundle: the point
// is to compare the files a translator edits, and the bundle exposes no way to
// enumerate what it loaded.
func catalogKeys(t *testing.T, l Locale) map[string]bool {
	t.Helper()
	b, err := catalogFS.ReadFile("locales/" + string(l) + ".toml")
	if err != nil {
		t.Fatalf("reading catalog for %s: %v", l, err)
	}
	var raw map[string]any
	if err := toml.Unmarshal(b, &raw); err != nil {
		t.Fatalf("parsing catalog for %s: %v", l, err)
	}
	out := map[string]bool{}
	flatten("", raw, out)
	return out
}

// flatten walks nested TOML tables into the dotted IDs go-i18n derives from
// them. A table holding only CLDR plural-form keys is one pluralized message,
// not a group, so it stops there.
func flatten(prefix string, m map[string]any, out map[string]bool) {
	if prefix != "" && isPluralTable(m) {
		out[prefix] = true
		return
	}
	for k, v := range m {
		id := k
		if prefix != "" {
			id = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flatten(id, sub, out)
			continue
		}
		out[id] = true
	}
}

func isPluralTable(m map[string]any) bool {
	forms := map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}
	for k := range m {
		if !forms[k] {
			return false
		}
	}
	return len(m) > 0
}
