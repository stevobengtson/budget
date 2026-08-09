package views

// Asset URLs carry a content hash so a changed file is fetched immediately while
// an unchanged one stays cached. Without it, /static/app.css is a stable URL
// that browsers hold on to across restarts — a CSS edit would appear to do
// nothing until a manual hard reload, which is a slow way to learn that your
// change did work.
//
// The hashes are computed once at startup from the embedded static FS (see
// assetHashes in the web package, which calls SetAssetHashes) rather than
// generated at build time, so there is no separate manifest to keep in step.

var assetHashes map[string]string

// SetAssetHashes installs the path→hash map. Called once during server
// construction, before any request is served, so the map is only ever read
// afterwards and needs no lock.
func SetAssetHashes(m map[string]string) { assetHashes = m }

// Asset returns a static path with its content hash appended, e.g.
// "/static/app.css?v=1f3c9a2b". Unknown paths (and the zero state before the
// hashes are installed, as in view unit tests) are returned unchanged, so a
// missing hash degrades to today's behaviour rather than a broken URL.
func Asset(path string) string {
	if h, ok := assetHashes[path]; ok {
		return path + "?v=" + h
	}
	return path
}
