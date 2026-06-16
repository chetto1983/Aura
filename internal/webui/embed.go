package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

// distFS holds the committed Vite build output that the single-binary host serves.
// The `all:` prefix is load-bearing: a bare `//go:embed dist` silently drops files
// whose names begin with `.` or `_` — and vite-plugin-pwa/Workbox emit such dotfiles
// (the service worker + precache manifest). `all:dist` guarantees nothing is dropped.
//
//go:embed all:dist
var distFS embed.FS

// distRoot is the embed prefix the build tree lives under.
const distRoot = "dist"

// Sub returns the dist/ subtree rooted so index.html is served from "/".
func Sub() (fs.FS, error) { return fs.Sub(distFS, distRoot) }

// Handler serves the embedded shell as a static file tree. http.FileServerFS sets
// Content-Type from each file's extension via the stdlib mime table, so .js/.css/
// .webmanifest/.png resolve without manual MIME wiring.
//
// Phase-23 scope is the static placeholder only. The SPA catch-all /
// index.html-fallback for client-side routes and the API/agent/health route
// exclusion are Phase 24 (WEB-01) — see doc.go. DO NOT add fallback logic here.
func Handler() (http.Handler, error) {
	sub, err := Sub()
	if err != nil {
		return nil, err
	}
	return http.FileServerFS(sub), nil
}
