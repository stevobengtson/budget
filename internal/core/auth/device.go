package auth

import "strings"

// DeviceLabel turns a user agent into something a person can recognise in the
// active-sessions list ("Chrome on macOS"). It is stored once, at session
// creation, because the point is to describe the device the user is trying to
// identify — not whatever agent happened to make the most recent request.
//
// Deliberately a handful of substring checks rather than a user-agent parsing
// dependency: the output is a hint for a human deciding which row to revoke,
// and being wrong about an obscure browser costs nothing. Order matters, since
// most user agents claim to be several browsers at once.
func DeviceLabel(ua string) string {
	if strings.TrimSpace(ua) == "" {
		return ""
	}
	browser := firstMatch(ua, []pair{
		{"Edg/", "Edge"},
		{"OPR/", "Opera"},
		{"Firefox/", "Firefox"},
		{"Chrome/", "Chrome"}, // after Edge/Opera, which both embed "Chrome/"
		{"Safari/", "Safari"}, // after Chrome, which embeds "Safari/"
	})
	os := firstMatch(ua, []pair{
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"Android", "Android"},
		{"Mac OS X", "macOS"},
		{"Windows", "Windows"},
		{"Linux", "Linux"},
	})

	switch {
	case browser != "" && os != "":
		return browser + " on " + os
	case browser != "":
		return browser
	case os != "":
		return os
	default:
		return ""
	}
}

type pair struct{ needle, label string }

func firstMatch(s string, pairs []pair) string {
	for _, p := range pairs {
		if strings.Contains(s, p.needle) {
			return p.label
		}
	}
	return ""
}
