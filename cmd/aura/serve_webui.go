// serve_webui.go wires the embedded operator SPA into the running daemon (FND-02 +
// WEB-01). The single-binary host serves the committed Vite build (internal/webui) at
// "/", additively, on the SAME loopback http.Server that already carries the AG-UI
// gateway — the embed mount adds no new listener and no new bind (T-23-06).
//
// Precedence is the whole design: a Go 1.22 http.ServeMux gives a longer/more
// specific registered pattern priority over the catch-all "/", so registering the
// explicit AG-UI route prefixes (/healthz, /readyz, /debug/vars, /metrics,
// /agent/run, /threads/) to the AG-UI handler and the integrations proxy subtree
// (/api/integrations/) ahead of "/" keeps those routes authoritative while everything
// else falls through to the embed.
//
// WEB-01: the "/" catch-all is an SPA-fallback, not a bare static tree. An unknown
// CLIENT route returns index.html (React Router resolves deep links); an excluded
// API/agent/health prefix returns a real 404 so the SPA shell never leaks to an API
// client (SC1). The exclusion set is SINGLE-SOURCED here — fallbackExcludedPrefixes()
// derives it from the AG-UI namespaces + the integrations subtree + the forward-compat
// "/api/" carve-out and passes a copy into webui.Handler, so the parent-mux
// registration and the fallback exclusion cannot drift (Pitfall 6 / T-24-08).
//
// The "/api/" carve-out is an EXCLUSION prefix ONLY — it is NOT registered on the mux.
// "/api/integrations/" is already mounted; a second mux.Handle("/api/", ...) would
// collide with / shadow that subtree (T-24-07). Adding "/api/" only to the fallback
// exclusion makes "/api/anything" 404 today and lets a real /api/* route register
// tomorrow without touching the fallback.
//
// internal/webui stays leaf-level (it imports no other internal/* package), an
// invariant scripts/agui_boundary_check.sh enforces via a dependency-closure
// assertion on the webui package.
package main

import (
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/webui"
)

// aguiRoutePrefixes are the route patterns the AG-UI gateway owns. Registered on
// the parent mux ahead of the "/" catch-all, Go 1.22 ServeMux precedence keeps them
// authoritative — a request to any of these reaches the AG-UI handler, never the
// embed. The trailing-slash "/threads/" is a subtree pattern (matches
// /threads/{id}/messages); the rest are exact paths.
var aguiRoutePrefixes = []string{
	"/healthz",
	"/readyz",
	"/debug/vars",
	"/metrics",
	"/agent/run",
	"/threads/",
}

// fallbackExcludedPrefixes is the SINGLE source of the SPA-fallback exclusion set:
// any request path under one of these returns a real 404 from internal/webui rather
// than the SPA shell (WEB-01 / SC1). It mirrors aguiRoutePrefixes (with the whole
// /agent/ namespace excluded — a typo like /agent/typo must 404 as backend, not fall
// back to the shell), adds the integrations proxy subtree, and adds the forward-compat
// "/api/" carve-out (which also subsumes /api/integrations/). It is passed into
// webui.Handler so the fallback never hard-codes a second list that could drift from
// the mux registration above (Pitfall 6 / T-24-08).
func fallbackExcludedPrefixes() []string {
	return []string{
		"/healthz",
		"/readyz",
		"/debug/vars",
		"/metrics",
		"/agent/",   // whole AG-UI agent namespace (mux registers the exact /agent/run)
		"/threads/", // AG-UI threads subtree
		integrationsRoutePrefix,
		"/api/", // forward-compat carve-out; exclusion-only, never a mux registration
	}
}

// newServeHandler builds the parent http.Handler for the daemon's single loopback
// server: the AG-UI route prefixes delegate to aguiHandler (the agui Server.Mux), the
// integrations proxy subtree mounts ahead of "/", and the catch-all "/" serves the
// SPA-fallback embed host (unknown client route -> index.html; excluded prefix -> 404).
// A webui.Handler failure (an embed sub error, which a committed dist makes
// unreachable) is returned so bootServe fails the daemon boot cleanly rather than
// mounting a half-wired host.
func newServeHandler(aguiHandler http.Handler) (http.Handler, error) {
	static, err := webui.Handler(fallbackExcludedPrefixes())
	if err != nil {
		return nil, fmt.Errorf("webui handler: %w", err)
	}
	mux := http.NewServeMux()
	for _, prefix := range aguiRoutePrefixes {
		mux.Handle(prefix, aguiHandler)
	}
	// The integrations admin proxy (cockpit connect data plane) mounts ahead of the
	// "/" embed catch-all; Go 1.22 longest-pattern precedence keeps it authoritative.
	// NOTE: "/api/" is deliberately NOT registered here — it lives only in the
	// fallback exclusion set, so registering it would collide with this subtree.
	mux.Handle(integrationsRoutePrefix, newIntegrationsProxy())
	mux.Handle("/", static)
	return mux, nil
}
