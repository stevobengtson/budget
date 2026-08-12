package i18n

import (
	"embed"
	"log/slog"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	toml "github.com/pelletier/go-toml/v2"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var catalogFS embed.FS

// M is a set of named values interpolated into a message, referenced in the
// catalog with Go template syntax: "Welcome back, {{.Name}}".
type M map[string]any

var (
	bundle      *goi18n.Bundle
	localizers  map[Locale]*goi18n.Localizer
	bundleOnce  sync.Once
	missingSeen sync.Map // message ID -> struct{}, so we log each miss once
)

// load builds the message bundle from the embedded catalogs. It runs once, on
// the first translation, and panics on a malformed catalog: a catalog that does
// not parse is a build-time mistake that would otherwise turn every string in
// the app into a raw message ID at runtime.
func load() {
	bundle = goi18n.NewBundle(language.MustParse(string(Default)))
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	entries, err := catalogFS.ReadDir("locales")
	if err != nil {
		panic("i18n: reading embedded locales: " + err.Error())
	}
	for _, e := range entries {
		if _, err := bundle.LoadMessageFileFS(catalogFS, "locales/"+e.Name()); err != nil {
			panic("i18n: loading catalog " + e.Name() + ": " + err.Error())
		}
	}

	localizers = make(map[Locale]*goi18n.Localizer, len(Supported))
	for _, l := range Supported {
		// Listing Default second makes it the fallback: a key missing from a
		// translation renders in English rather than as a raw ID.
		localizers[l] = goi18n.NewLocalizer(bundle, string(l), string(Default))
	}
}

// localizerFor returns the shared localizer for a locale. Localizers are
// read-only once built, so one per locale is safe to share across requests.
func localizerFor(l Locale) *goi18n.Localizer {
	bundleOnce.Do(load)
	if lz, ok := localizers[l]; ok {
		return lz
	}
	return localizers[Default]
}

// translate looks up a message, interpolating data and selecting a plural form
// when count is non-nil.
//
// A lookup that fails returns the message ID itself. Rendering "budget.title"
// in the page is ugly on purpose — it is visible in a screenshot and in the
// view tests, where a silent English fallback would not be — while still
// leaving the page usable.
func translate(l Locale, id string, data M, count any) string {
	cfg := &goi18n.LocalizeConfig{MessageID: id}
	if data != nil {
		cfg.TemplateData = map[string]any(data)
	}
	if count != nil {
		cfg.PluralCount = count
		// Plural messages nearly always want the count in the text, so make it
		// available as {{.Count}} without every call site repeating it.
		if cfg.TemplateData == nil {
			cfg.TemplateData = map[string]any{"Count": count}
		} else if _, ok := data["Count"]; !ok {
			cfg.TemplateData.(map[string]any)["Count"] = count
		}
	}

	s, err := localizerFor(l).Localize(cfg)
	if err != nil {
		if _, dup := missingSeen.LoadOrStore(id, struct{}{}); !dup {
			slog.Warn("i18n: message lookup failed", "id", id, "locale", l, "err", err)
		}
		return id
	}
	return s
}
