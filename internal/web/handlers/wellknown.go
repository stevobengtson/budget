package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AssociationConfig is what the two /.well-known documents are built from.
type AssociationConfig struct {
	// AppleAppIDs are "<TEAMID>.<bundle id>".
	AppleAppIDs []string
	// AndroidPackage is the application id, e.g. "ca.pigglet.budget".
	AndroidPackage string
	// AndroidFingerprints are the signing certificates' SHA-256 digests in
	// colon-separated hex.
	AndroidFingerprints []string
}

// AppleAppSiteAssociation serves /.well-known/apple-app-site-association.
//
// Apple fetches this to decide whether the app may share credentials with the
// site and open its links. Every part of the delivery matters and fails
// silently if wrong: it must be served as application/json, at exactly this
// path with no file extension, over a 200 with no redirect.
//
// webcredentials is what lets a passkey registered on the site be offered in
// the app. applinks is not needed for passkeys but is what makes emailed links
// open in the app rather than the browser, which the magic-link and OAuth
// phases will both want.
func (h *Handlers) AppleAppSiteAssociation(c *gin.Context) {
	apps := h.assoc.AppleAppIDs
	if apps == nil {
		apps = []string{}
	}
	doc := gin.H{
		"webcredentials": gin.H{"apps": apps},
		"applinks": gin.H{
			"apps": []string{},
			"details": []gin.H{{
				"appIDs":     apps,
				"components": []gin.H{{"/": "/verify*"}, {"/": "/reset*"}, {"/": "/login*"}},
			}},
		},
	}
	// Apple caches this aggressively via its CDN; a short max-age keeps a
	// correction from taking days to reach devices.
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, doc)
}

// AssetLinks serves /.well-known/assetlinks.json.
//
// Two relations, not one: handle_all_urls makes the app open our links, and
// get_login_creds is the one that actually permits passkey sharing. Omitting
// the second is a common way for Android passkeys to fail while everything
// else about the association looks correct.
//
// The fingerprints here are colon-separated hex, while the SAME certificates
// appear base64url-encoded in the passkey origin allowlist. Two encodings of
// one value in two files is exactly the kind of thing that gets transposed.
func (h *Handlers) AssetLinks(c *gin.Context) {
	if h.assoc.AndroidPackage == "" {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}
	fingerprints := h.assoc.AndroidFingerprints
	if fingerprints == nil {
		fingerprints = []string{}
	}
	doc := []gin.H{{
		"relation": []string{
			"delegate_permission/common.handle_all_urls",
			"delegate_permission/common.get_login_creds",
		},
		"target": gin.H{
			"namespace":                "android_app",
			"package_name":             h.assoc.AndroidPackage,
			"sha256_cert_fingerprints": fingerprints,
		},
	}}
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, doc)
}
