package handlers

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sbengtson/budget/internal/core/i18n"
	"github.com/sbengtson/budget/internal/web/views"
)

// The sitemap lists every public page in every language.
//
// It covers only the marketing and legal pages: everything else is behind a
// session and has nothing to offer a crawler. The pages it does list are the
// ones that exist at one URL per language, which is exactly the case a sitemap
// earns its keep — each language's URL is announced, and each entry names its
// counterparts so a crawler treats them as translations rather than duplicates.

// urlset is the sitemaps.org document, with the xhtml namespace that carries
// the per-language alternates.
type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	XHTML   string       `xml:"xmlns:xhtml,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc        string          `xml:"loc"`
	Alternates []sitemapAltURL `xml:"xhtml:link"`
}

type sitemapAltURL struct {
	Rel      string `xml:"rel,attr"`
	Hreflang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

// Sitemap serves /sitemap.xml.
//
// Every language variant of a page gets its own <url> entry, and every entry
// lists the full set of alternates including itself — that reciprocity is what
// the spec asks for, and a one-way declaration is ignored.
//
// No <lastmod>: the marketing copy has no meaningful modification date to
// report, and a date that does not track real edits is worse than none, since
// crawlers learn to distrust it.
func (h *Handlers) Sitemap(c *gin.Context) {
	base := strings.TrimSuffix(h.baseURL, "/")

	// Built once per request and shared by every entry: the alternates are the
	// same set of URLs for a given page whichever language's entry names them.
	doc := urlset{
		NS:    "http://www.sitemaps.org/schemas/sitemap/0.9",
		XHTML: "http://www.w3.org/1999/xhtml",
	}
	for _, page := range views.PublicPages {
		alts := make([]sitemapAltURL, 0, len(i18n.Supported)+1)
		for _, l := range i18n.Supported {
			alts = append(alts, sitemapAltURL{
				Rel: "alternate", Hreflang: string(l), Href: base + views.PublicHref(l, page),
			})
		}
		// x-default is what a crawler serves when it can match no language.
		alts = append(alts, sitemapAltURL{
			Rel: "alternate", Hreflang: "x-default", Href: base + views.PublicHref(i18n.Default, page),
		})

		for _, l := range i18n.Supported {
			doc.URLs = append(doc.URLs, sitemapURL{
				Loc:        base + views.PublicHref(l, page),
				Alternates: alts,
			})
		}
	}

	c.Header("Content-Type", "application/xml; charset=utf-8")
	c.Status(http.StatusOK)
	if _, err := c.Writer.WriteString(xml.Header); err != nil {
		return
	}
	enc := xml.NewEncoder(c.Writer)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		// Headers are already out, so there is no status left to change; log via
		// the request's error chain and let the truncated body speak for itself.
		_ = c.Error(err)
	}
}
