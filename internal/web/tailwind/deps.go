//go:build tools

// Package tailwind pins templUI so `go mod tidy` retains it in go.mod. templUI
// is consumed by the Tailwind build (its component classes are scanned from the
// module cache via @source in input.css) before any Go code imports it. Real
// component imports land in later migration tasks; this pin can stay.
package tailwind

import _ "github.com/templui/templui/components/button"
