package i18n

import "context"

type ctxKey int

const localeKey ctxKey = iota

// WithLocale returns ctx carrying the request's locale. The web middleware sets
// it on the request context, which Templ then passes to every component as
// `ctx`, so views translate without a localizer parameter.
func WithLocale(ctx context.Context, l Locale) context.Context {
	return context.WithValue(ctx, localeKey, l)
}

// LocaleFrom returns the locale set by WithLocale, or Default when absent —
// which covers background jobs and tests that never went through the middleware.
func LocaleFrom(ctx context.Context) Locale {
	if l, ok := ctx.Value(localeKey).(Locale); ok && l.Valid() {
		return l
	}
	return Default
}

// T translates a message with no interpolation and no plural form.
//
//	{ i18n.T(ctx, "budget.title") }
func T(ctx context.Context, id string) string {
	return translate(LocaleFrom(ctx), id, nil, nil)
}

// Tf translates a message, interpolating named values referenced in the catalog
// as Go template fields.
//
//	{ i18n.Tf(ctx, "budget.greeting", i18n.M{"Name": user.Name}) }
func Tf(ctx context.Context, id string, data M) string {
	return translate(LocaleFrom(ctx), id, data, nil)
}

// Tn translates a pluralized message, selecting the form for count under the
// locale's CLDR plural rules. The count is available in the catalog as
// {{.Count}}.
//
// The rules genuinely differ: English takes the "one" form only at exactly 1,
// while French also takes it at 0 — "0 transaction", not "0 transactions".
//
//	{ i18n.Tn(ctx, "transactions.count", n) }
func Tn(ctx context.Context, id string, count any) string {
	return translate(LocaleFrom(ctx), id, nil, count)
}

// Tnf translates a pluralized message that also interpolates named values.
func Tnf(ctx context.Context, id string, count any, data M) string {
	return translate(LocaleFrom(ctx), id, data, count)
}

// EveryLocale renders a message in all supported locales at once, the context's
// own locale first, with identical renderings collapsed to one.
//
// It exists for the signed-out auth pages, which have to be usable by someone
// whose language we guessed wrong: there is no signed-in preference to read, and
// a visitor who cannot read the guess cannot get far enough to correct it. So
// those pages show every language rather than picking one.
//
// This is a stopgap sized for two languages. Rendering every string N times does
// not survive a third, at which point these pages need a language switcher
// instead — see the callers in the auth views.
func EveryLocale(ctx context.Context, id string) []string {
	return everyLocale(LocaleFrom(ctx), id, nil)
}

// EveryLocaleF is EveryLocale for a message that interpolates named values.
func EveryLocaleF(ctx context.Context, id string, data M) []string {
	return everyLocale(LocaleFrom(ctx), id, data)
}

func everyLocale(first Locale, id string, data M) []string {
	order := make([]Locale, 0, len(Supported))
	order = append(order, first)
	for _, l := range Supported {
		if l != first {
			order = append(order, l)
		}
	}

	out := make([]string, 0, len(order))
	for _, l := range order {
		s := translate(l, id, data, nil)
		// A word that is the same in both languages ("Notes", "Internet") must
		// not render as "Notes / Notes".
		if !contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// TIn translates a message in an explicit locale, for the cases where the
// request context is not the right authority.
//
// The first-run wizard is exactly that case: it seeds the starter budget in the
// language the user just picked, in the same request that saves the choice, so
// the context still carries whatever locale they arrived with.
func TIn(l Locale, id string) string {
	return translate(l, id, nil, nil)
}

// TOr translates a message, falling back to a caller-supplied string when the
// key is absent from every catalog.
//
// It exists for text that lives in the database but is authored by us rather
// than by the user — the add-on catalog's names and descriptions, for example.
// Those need translating, but the row is the source of truth for which add-ons
// exist, so a new one must render its English database text rather than a raw
// message ID while its catalog entry is still being written.
//
// Do not reach for this to paper over a missing key in ordinary UI text: there
// the raw ID showing up is the point, because it is how the gap gets noticed.
func TOr(ctx context.Context, id string, fallback string) string {
	if s := translate(LocaleFrom(ctx), id, nil, nil); s != id {
		return s
	}
	return fallback
}
