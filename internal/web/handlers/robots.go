package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// robotsRules is the body of /robots.txt, minus the Sitemap line (which needs
// the configured origin and so is appended at request time).
//
// The default is allow: every path not named here is crawlable, which keeps the
// public marketing and legal pages — the only ones with anything to index —
// open in both languages without having to list them.
//
// What is disallowed falls into two groups:
//
//   - Signed-in application paths. A crawler gets a redirect to /login from all
//     of them, so nothing leaks, but it spends crawl budget discovering that on
//     every URL a marketing page happens to link to.
//   - Paths that carry a single-use token in the query string (/verify, /reset)
//     or that accept a webhook. Those must never be fetched by anything that
//     found the URL somewhere it should not have.
//
// Prefix semantics: each rule matches any path starting with it, so /account
// covers /account/billing and /accounts covers /accounts/new.
const robotsRules = `User-agent: *
Disallow: /account
Disallow: /admin
Disallow: /api/
Disallow: /billing
Disallow: /budget
Disallow: /categories
Disallow: /forgot
Disallow: /login
Disallow: /logout
Disallow: /paydown
Disallow: /reset
Disallow: /signup
Disallow: /transactions
Disallow: /verify
Disallow: /webhooks/
Disallow: /welcome
`

// Robots serves /robots.txt.
//
// It is generated rather than shipped as a static file for one reason: the
// Sitemap directive has to be an absolute URL, and the origin is configuration.
func (h *Handlers) Robots(c *gin.Context) {
	base := strings.TrimSuffix(h.baseURL, "/")

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, "%s\nSitemap: %s/sitemap.xml\n", robotsRules, base)
}
