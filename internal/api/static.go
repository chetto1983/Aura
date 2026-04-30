package api

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// StaticHandler serves the embedded SPA from web/dist with a fallback to
// index.html for any path that doesn't match a real asset and isn't an /api
// route. The fallback is what makes deep-link refresh work for client-side
// react-router routes like /wiki/source-paper.
//
// Returns nil + ErrNoStaticAssets when dist/ is empty (e.g., before the
// frontend has been built). Callers should treat this as a soft warning,
// not a fatal error, so the bot still starts when the developer hasn't
// run `make web-build` yet.
func StaticHandler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, ErrNoStaticAssets
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Reserved prefixes that must never be SPA-shadowed. /api/, /health,
		// /status, and /telegram are wired separately on the same mux; we only
		// get here for paths the other handlers didn't claim.
		clean := path.Clean(r.URL.Path)
		if strings.HasPrefix(clean, "/api/") || clean == "/health" || clean == "/status" || clean == "/telegram" {
			http.NotFound(w, r)
			return
		}

		// If the request resolves to a real file in dist, serve it.
		rel := strings.TrimPrefix(clean, "/")
		if rel == "" {
			rel = "index.html"
		}
		if _, err := fs.Stat(sub, rel); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fallback: serve index.html so the SPA can route client-side.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}), nil
}

// ErrNoStaticAssets is returned by StaticHandler when dist/ has not been
// populated (e.g., before the first `npm run build`). cmd/aura logs this
// and continues — the API still works; only the SPA is unavailable.
var ErrNoStaticAssets = errors.New("api: no static assets in dist/ (run npm run build)")
