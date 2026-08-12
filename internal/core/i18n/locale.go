// Package i18n holds the application's translation catalogs and the locale
// plumbing shared by the web UI and the core formatting helpers.
//
// A request carries its locale on the standard context (see WithLocale), which
// is what Templ hands every component as `ctx`. That keeps translation out of
// every component's parameter list: a view calls i18n.T(ctx, "key") directly
// rather than threading a localizer down through the whole tree.
package i18n

import (
	"net/http"
	"strings"
)

// Locale is a supported BCP 47 language tag. The set is deliberately closed —
// every locale needs a hand-written catalog plus number/date tables, so an
// unknown tag is always a bug rather than something to degrade gracefully into.
type Locale string

const (
	// EnCA is Canadian English, the source language for every message.
	EnCA Locale = "en-CA"
	// FrCA is Canadian French.
	FrCA Locale = "fr-CA"
)

// Default is the locale used when nothing else resolves.
const Default = EnCA

// Supported lists every locale the app ships, in menu order.
var Supported = []Locale{EnCA, FrCA}

// Name returns the locale's own endonym, for a language picker. A picker is the
// one place a language must never be labelled in the *current* locale — someone
// who cannot read the current one still has to find their own.
func (l Locale) Name() string {
	switch l {
	case FrCA:
		return "Français (Canada)"
	default:
		return "English (Canada)"
	}
}

// ShortName is the compact endonym for a language switch that has to fit in a
// header — "Français" rather than "Français (Canada)". Like Name, it is always
// the language's own word for itself.
func (l Locale) ShortName() string {
	switch l {
	case FrCA:
		return "Français"
	default:
		return "English"
	}
}

// Valid reports whether l is a supported locale.
func (l Locale) Valid() bool {
	for _, s := range Supported {
		if s == l {
			return true
		}
	}
	return false
}

// Parse maps a tag to a supported locale. It matches the full tag first, then
// falls back to the base language, so "fr", "fr-FR" and "fr-CA" all land on
// fr-CA — we ship one French, and serving a French speaker English because
// their browser says fr-FR would be the worse failure. ok is false when nothing
// matched, letting the caller decide whether to keep looking.
func Parse(tag string) (Locale, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return Default, false
	}
	for _, l := range Supported {
		if strings.EqualFold(string(l), tag) {
			return l, true
		}
	}
	base, _, _ := strings.Cut(tag, "-")
	for _, l := range Supported {
		lb, _, _ := strings.Cut(string(l), "-")
		if strings.EqualFold(lb, base) {
			return l, true
		}
	}
	return Default, false
}

// FromAcceptLanguage picks the best supported locale from an Accept-Language
// header, honouring q-weights by taking the first match in the header's own
// order. Browsers already send the list in descending preference, so the first
// tag we support is the user's most-preferred available language.
//
// Returns Default and false when the header is missing or names nothing we
// speak.
func FromAcceptLanguage(header string) (Locale, bool) {
	for _, part := range strings.Split(header, ",") {
		tag, _, _ := strings.Cut(part, ";") // drop any ";q=0.8"
		tag = strings.TrimSpace(tag)
		if tag == "" || tag == "*" {
			continue
		}
		if l, ok := Parse(tag); ok {
			return l, true
		}
	}
	return Default, false
}

// CookieName is the cookie that remembers a language choice made before
// sign-in (on the login and registration pages), and that mirrors the stored
// preference afterwards so a signed-out visit keeps the same language.
const CookieName = "budget_locale"

// FromRequest resolves the locale for a request that has no signed-in user:
// the explicit cookie choice first, then the browser's Accept-Language.
func FromRequest(r *http.Request) Locale {
	if c, err := r.Cookie(CookieName); err == nil {
		if l, ok := Parse(c.Value); ok {
			return l
		}
	}
	l, _ := FromAcceptLanguage(r.Header.Get("Accept-Language"))
	return l
}
