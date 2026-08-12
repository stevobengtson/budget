package views

import (
	"context"
	"strings"

	"github.com/sbengtson/budget/internal/core/i18n"
)

// The public marketing and legal pages are served at one URL per language: the
// English set at the root, the French set under /fr. So every internal link
// between them has to stay inside the language the visitor is reading, and each
// page has to declare its counterpart for crawlers.

// marketingShot picks the product screenshot for the reader's language. The
// app in the picture is the app they would get, so an English screenshot on the
// French page undercuts the page it sits on.
//
// A language with no screenshots of its own falls back to the default locale's
// rather than emitting a URL that 404s — a new language should ship with a
// readable page before it ships with its own screenshots.
func marketingShot(ctx context.Context, name string) string {
	path := "/static/marketing/" + string(i18n.LocaleFrom(ctx)) + "/" + name + ".png"
	if AssetExists(path) {
		return path
	}
	return "/static/marketing/" + string(i18n.Default) + "/" + name + ".png"
}

// PublicPages are the canonical (English) paths of the public marketing and
// legal pages, in the order a sitemap should list them.
//
// It is the single source of truth: the router registers these paths under each
// language, and the sitemap enumerates the same list. Adding a page here and
// nowhere else is what should happen — forgetting one or the other is the
// mistake this prevents.
var PublicPages = []string{"/", "/privacy", "/terms", "/refund", "/contact"}

// publicHref maps a canonical (English) public path to its URL in the context's
// locale: "/privacy" stays "/privacy" in English and becomes "/fr/privacy" in
// French. App routes (/login, /signup) are NOT public pages and are passed
// through untouched — they are bilingual already and exist at one URL.
func publicHref(ctx context.Context, path string) string {
	return PublicHref(i18n.LocaleFrom(ctx), path)
}

// PublicHref maps a canonical public path into a locale's URL. Exported for the
// sitemap, which has to emit every language's URL regardless of the request.
func PublicHref(l i18n.Locale, path string) string {
	if l == i18n.Default {
		return path
	}
	if path == "/" {
		return "/" + string(localePathSegment(l))
	}
	return "/" + string(localePathSegment(l)) + path
}

// localePathSegment is the URL segment a non-default locale lives under. Only
// the base language, so the French pages read /fr/privacy rather than /fr-CA/…
func localePathSegment(l i18n.Locale) string {
	base, _, _ := strings.Cut(string(l), "-")
	return base
}
