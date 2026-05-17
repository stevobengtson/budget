// Package cn is a small class-name concatenation helper used by shadcntempl
// components. It mirrors the `cn(...)` utility shipped with shadcn/ui, but
// without tailwind-merge semantics — last writer wins via duplication, which
// is good enough for component-level overrides.
package cn

import "strings"

// Join concatenates non-empty class fragments with single spaces, preserving
// order. Empty inputs are skipped.
func Join(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}
