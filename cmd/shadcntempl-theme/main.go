// Command shadcntempl-theme rewrites the :root and .dark blocks inside a
// shadcntempl theme.css from a shadcn theme preset.
//
// Inputs (mutually exclusive, one is required):
//
//	-url    URL to a shadcn registry theme JSON (object with a "cssVars"
//	        field, shape {"theme": {...}, "light": {...}, "dark": {...}}).
//	-file   Path to a local file. Either the same JSON shape OR a raw CSS
//	        file containing :root { ... } and .dark { ... } blocks.
//	-preset shadcn create preset code (e.g. b6FTKD8F6). EXPERIMENTAL — the
//	        shadcn website resolves preset codes client-side, so this flag
//	        only works if a public endpoint is known; otherwise the command
//	        prints instructions and exits non-zero.
//
// Output:
//
//	-out    Path to the theme.css to rewrite (default: pkg/shadcntempl/theme/theme.css).
//
// The file is updated in place; the @theme inline mapping in
// pkg/shadcntempl/tailwind/input.css is left untouched.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "", "shadcn registry theme JSON URL")
	file := flag.String("file", "", "local theme JSON or CSS file")
	preset := flag.String("preset", "", "shadcn create preset code (experimental)")
	out := flag.String("out", "pkg/shadcntempl/theme/theme.css", "theme.css path to rewrite")
	flag.Parse()

	if err := run(*url, *file, *preset, *out); err != nil {
		fmt.Fprintln(os.Stderr, "shadcntempl-theme:", err)
		os.Exit(1)
	}
}

func run(url, file, preset, outPath string) error {
	if url == "" && file == "" && preset == "" {
		flag.Usage()
		return errors.New("one of -url, -file, -preset is required")
	}
	if preset != "" && url == "" && file == "" {
		return fmt.Errorf(`preset codes from https://ui.shadcn.com/create are resolved client-side and have no known stable HTTP endpoint.
Open the create page in a browser, copy the generated CSS, save it as theme.css, then re-run with -file <path> (or pipe a registry JSON URL via -url).
Tried preset: %s`, preset)
	}

	body, err := loadSource(url, file)
	if err != nil {
		return err
	}

	light, dark, err := parseTokens(body)
	if err != nil {
		return err
	}
	if len(light) == 0 && len(dark) == 0 {
		return errors.New("no CSS variables found in source")
	}

	current, err := os.ReadFile(outPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", outPath, err)
	}

	updated := rewriteBlock(string(current), ":root", light)
	updated = rewriteBlock(updated, ".dark", dark)

	if err := os.WriteFile(outPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf("wrote %s (light=%d tokens, dark=%d tokens)\n", outPath, len(light), len(dark))
	return nil
}

func loadSource(url, file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// parseTokens accepts either a shadcn registry JSON payload or a raw CSS file
// and returns the (light, dark) token maps. Token names are stored without
// the leading "--".
func parseTokens(body []byte) (light, dark map[string]string, err error) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		return parseJSON(body)
	}
	return parseCSS(string(body))
}

// shadcn registry payloads vary; we accept both the legacy theme shape and
// the v2 registry item shape.
type registryPayload struct {
	CSSVars *struct {
		Theme map[string]string `json:"theme"`
		Light map[string]string `json:"light"`
		Dark  map[string]string `json:"dark"`
	} `json:"cssVars,omitempty"`
	Light map[string]string `json:"light,omitempty"`
	Dark  map[string]string `json:"dark,omitempty"`
}

func parseJSON(body []byte) (map[string]string, map[string]string, error) {
	var p registryPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, nil, fmt.Errorf("parse json: %w", err)
	}
	light := map[string]string{}
	dark := map[string]string{}
	if p.CSSVars != nil {
		for k, v := range p.CSSVars.Theme {
			light[k] = v
			dark[k] = v
		}
		for k, v := range p.CSSVars.Light {
			light[k] = v
		}
		for k, v := range p.CSSVars.Dark {
			dark[k] = v
		}
	}
	for k, v := range p.Light {
		light[k] = v
	}
	for k, v := range p.Dark {
		dark[k] = v
	}
	return light, dark, nil
}

var (
	rootBlockRe = regexp.MustCompile(`(?s):root\s*\{([^}]*)\}`)
	darkBlockRe = regexp.MustCompile(`(?s)\.dark\s*\{([^}]*)\}`)
	tokenRe     = regexp.MustCompile(`--([a-zA-Z0-9-]+)\s*:\s*([^;]+);`)
)

func parseCSS(src string) (map[string]string, map[string]string, error) {
	light := map[string]string{}
	dark := map[string]string{}
	if m := rootBlockRe.FindStringSubmatch(src); len(m) == 2 {
		light = extractTokens(m[1])
	}
	if m := darkBlockRe.FindStringSubmatch(src); len(m) == 2 {
		dark = extractTokens(m[1])
	}
	return light, dark, nil
}

func extractTokens(block string) map[string]string {
	out := map[string]string{}
	for _, m := range tokenRe.FindAllStringSubmatch(block, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// rewriteBlock replaces the contents of the named CSS selector block
// (`:root` or `.dark`) inside `src` with the supplied tokens, preserving
// indentation and trailing newline.
func rewriteBlock(src, selector string, tokens map[string]string) string {
	if len(tokens) == 0 {
		return src
	}
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(selector)
	b.WriteString(" {\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  --%s: %s;\n", k, tokens[k])
	}
	b.WriteString("}")

	// Match selector + brace block, even across newlines. Escape selector
	// for regex (`.dark` has a literal dot).
	pattern := regexp.QuoteMeta(selector) + `\s*\{[^}]*\}`
	re := regexp.MustCompile(`(?s)` + pattern)
	if re.MatchString(src) {
		return re.ReplaceAllString(src, b.String())
	}
	// Selector missing — append.
	return strings.TrimRight(src, "\n") + "\n\n" + b.String() + "\n"
}
