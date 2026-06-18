package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// distFS holds the committed Vite build output that the single-binary host serves.
// The `all:` prefix is load-bearing: a bare `//go:embed dist` silently drops files
// whose names begin with `.` or `_`. The local PWA build plugin emits service-worker
// support files alongside the Vite bundle; `all:dist` guarantees nothing is dropped.
//
//go:embed all:dist
var distFS embed.FS

// distRoot is the embed prefix the build tree lives under.
const distRoot = "dist"

// Sub returns the dist/ subtree rooted so index.html is served from "/".
func Sub() (fs.FS, error) { return fs.Sub(distFS, distRoot) }

// Handler serves the embedded SPA shell with WEB-01 fallback semantics. apiPrefixes
// are the routes the daemon owns elsewhere (AG-UI gateway, the integrations proxy,
// the forward-compat /api/ carve-out) and MUST 404 as real API errors rather than
// leak the SPA shell (WEB-01 / SC1). The caller — cmd/aura/serve_webui.go — is the
// single source of that list (derived from aguiRoutePrefixes + integrationsRoutePrefix
// + "/api/"), so internal/webui never hard-codes a second list that could drift.
//
// Behavior:
//   - an excluded prefix (e.g. /api/nope, /agent/typo, /healthz...) → real 404;
//   - the empty path or a path with no embedded asset → index.html, so React Router
//     resolves deep client-route links (and renders its own in-app 404);
//   - a real embedded asset → that asset, with Content-Type from the stdlib mime
//     table (http.FileServerFS), so .js/.css/.webmanifest/.png resolve without
//     manual MIME wiring and the theme-before-paint markers survive the fallback.
//
// internal/webui stays leaf-level: this file imports only embed/io/fs/net/http/strings
// and never an internal/* package — scripts/agui_boundary_check.sh enforces it.
func Handler(apiPrefixes []string) (http.Handler, error) {
	sub, err := Sub()
	if err != nil {
		return nil, err
	}
	return spaFallback(sub, normalizePrefixes(apiPrefixes), http.FileServerFS(sub)), nil
}

// normalizePrefixes trims the leading "/" from each caller-supplied prefix so the
// comparison in spaFallback runs against the same no-leading-slash form as the
// trimmed request path. The caller may pass either "/agent/run" or "agent/run".
func normalizePrefixes(prefixes []string) []string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, strings.TrimPrefix(p, "/"))
	}
	return out
}

// indexHTML is the SPA shell path the fallback resolves unknown client routes to.
const indexHTML = "index.html"

// spaFallback is the SPA-fallback handler. It resolves a request path to a real 404
// (excluded API prefix), the SPA shell (unknown client route or "/"), or a real
// embedded asset. A real asset is delegated to fileSrv (http.FileServerFS) so MIME +
// path-cleaning stay stdlib; the SPA shell is written via http.ServeFileFS so it does
// NOT trigger the FileServer's index.html→"./" canonical redirect (which loops the
// catch-all). Resolution is always against the embedded fs.FS — never the host
// filesystem — so path traversal (../) cannot escape the embed (T-24-06).
func spaFallback(sub fs.FS, apiPrefixes []string, fileSrv http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		for _, api := range apiPrefixes {
			if api != "" && strings.HasPrefix(p, api) {
				http.NotFound(w, r) // real 404 — never the SPA shell (WEB-01 / SC1)
				return
			}
		}
		if p == "" || statMissing(sub, p) {
			// "/" or a deep client-route link → SPA shell (React Router resolves it).
			http.ServeFileFS(w, r, sub, indexHTML)
			return
		}
		if p == "manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		}
		fileSrv.ServeHTTP(w, r)
	}
}

// statMissing reports whether p has no regular file in the embedded FS (so the
// request is a client route, not a real asset).
func statMissing(sub fs.FS, p string) bool {
	info, err := fs.Stat(sub, p)
	return err != nil || info.IsDir()
}

// IsPublicAsset reports whether p resolves to a real embedded static asset — a hashed
// /assets/* bundle, the PWA service worker + manifest, or an icon — rather
// than the SPA shell (index.html) or an unknown client route. The web-auth boundary
// (internal/agui RequireAuth) serves these WITHOUT a session so the login page can load
// its bundle + styles before a cookie exists (D-03 "the login page + its static
// assets"). A single shared Vite bundle means the login page and the cockpit pull the
// SAME /assets/* files, so the whole static-asset set is public; the rendered shell at
// "/" still requires the session, so index.html is deliberately NOT an asset here.
func IsPublicAsset(p string) bool {
	sub, err := Sub()
	if err != nil {
		return false
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == indexHTML {
		return false
	}
	return !statMissing(sub, p)
}
