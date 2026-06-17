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

	"github.com/chetto1983/aura/internal/agui"
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

// agentRunCapability is the capability_grants name the mutating POST /agent/run route
// is gated on (D-04 / WEB-03). The seeded `local` identity holds the `*` wildcard so it
// passes regardless of the exact name; the name only becomes load-bearing when real
// grants arrive in Phase 28. It invents no governance write routes — those land later.
const agentRunCapability = "agent.run"

// conversationsRoutePrefix is the CHAT-02 conversation-management subtree (Phase 25),
// registered on the parent mux as a SPECIFIC subtree delegating to the AG-UI handler.
// It MUST stay a sibling of "/api/integrations/" under the "/api/" exclusion carve-out
// — never a bare "/api/", which would shadow the integrations proxy (T-24-07 / T-25-05).
const conversationsRoutePrefix = "/api/conversations/"

// conversationsListRoute is the exact (no trailing slash) list endpoint. Go 1.22
// ServeMux 301-redirects "/api/conversations" into the "/api/conversations/" subtree
// unless the exact path is also registered, so registering it explicitly keeps the
// list GET reachable without a redirect hop.
const conversationsListRoute = "/api/conversations"

// branchEditRoute / branchSelectRoute are the D-09 / CHAT-05 MUTATING branch re-run
// endpoints (plan 25-07). Re-running a (possibly background) thread is privileged
// (T-25-25 / V4), so both are interposed with RequireCapability exactly like POST
// /agent/run — the method+path-specific pattern wins Go 1.22 longest-pattern precedence
// over the bare "/api/conversations/" subtree (which carries the read GET /branches under
// RequireAuth only). The fork+re-run (edit) and the branch-select re-run both fire AFTER
// RequireAuth has bound the principal.
const (
	branchEditRoute   = "POST /api/conversations/{id}/edit"
	branchSelectRoute = "POST /api/conversations/{id}/branches/{branchSeq}/select"
)

// approvalsListRoute is the exact (no trailing slash) cross-thread pending read
// (APRV-01), registered as a SPECIFIC parent-mux path delegating to the AG-UI handler
// — a sibling of "/api/conversations/" + "/api/integrations/" under the "/api/"
// exclusion carve-out, NEVER a bare "/api/" (which would shadow the integrations proxy,
// T-25-05). It inherits RequireAuth from the whole-mux wrap below.
const approvalsListRoute = "/api/approvals"

// approvalsResolveRoute is the mutating resume/decline/cancel endpoint (APRV-02).
// Resuming or cancelling another thread's (possibly background) run is privileged
// (Security V4 / T-25-07), so it is interposed with RequireCapability exactly like
// "POST /agent/run": the method+path-specific pattern wins Go 1.22 longest-pattern
// precedence and the gate fires AFTER RequireAuth has bound the principal.
const approvalsResolveRoute = "POST /api/approvals/{token}/resolve"

// newServeHandler builds the parent http.Handler for the daemon's single loopback
// server: the AG-UI route prefixes delegate to aguiHandler (the agui Server.Mux), the
// integrations proxy subtree mounts ahead of "/", and the catch-all "/" serves the
// SPA-fallback embed host (unknown client route -> index.html; excluded prefix -> 404).
// A webui.Handler failure (an embed sub error, which a committed dist makes
// unreachable) is returned so bootServe fails the daemon boot cleanly rather than
// mounting a half-wired host.
//
// WEB-03 (D-03/D-04): the whole returned subtree is wrapped in agui.RequireAuth so the
// origin is private when a secret is configured — the public-path exceptions (the login
// route + its assets + GET /healthz) are handled INSIDE the middleware, not by leaving
// routes unwrapped. POST /login + POST /logout register as public credential endpoints.
// The only mutating route, POST /agent/run, is additionally interposed with
// agui.RequireCapability ahead of the AG-UI prefix loop (Go 1.22 method-pattern
// precedence makes "POST /agent/run" win over the bare "/agent/run") so the capability
// gate fires AFTER RequireAuth has bound the principal. When no secret is configured
// (loopback dev) RequireAuth is a no-op pass-through and the daemon serves exactly as
// before (the Plan-01 boot guard confines an unconfigured secret to loopback).
func newServeHandler(aguiHandler http.Handler, auth agui.AuthDeps) (http.Handler, error) {
	static, err := webui.Handler(fallbackExcludedPrefixes())
	if err != nil {
		return nil, fmt.Errorf("webui handler: %w", err)
	}
	mux := http.NewServeMux()
	// The mutating route is interposed with the capability gate FIRST: "POST /agent/run"
	// is a more specific pattern than the bare "/agent/run" the prefix loop registers, so
	// Go 1.22 longest-pattern precedence routes the POST through RequireCapability →
	// aguiHandler while other methods/paths under /agent/run fall to the prefix entry.
	mux.Handle("POST /agent/run", agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	for _, prefix := range aguiRoutePrefixes {
		mux.Handle(prefix, aguiHandler)
	}
	// Public credential endpoints (NOT behind the gate — RequireAuth's public-path set
	// lets the login route + assets through, and these POST handlers issue/clear the
	// cookie). They mount on the parent mux so they sit beside the AG-UI routes.
	mux.HandleFunc("POST /login", auth.LoginHandler())
	mux.HandleFunc("POST /logout", auth.LogoutHandler())
	// The CHAT-02 conversation-management subtree (Phase 25) delegates to the AG-UI
	// handler, which carries the /api/conversations/ routes on its own Server.Mux. It
	// is registered as the SPECIFIC subtree — NEVER a bare "/api/", which would shadow
	// "/api/integrations/" below (T-24-07 / T-25-05). Go 1.22 longest-pattern precedence
	// keeps both "/api/conversations/" and "/api/integrations/" authoritative side by
	// side over the "/" embed catch-all, and the "/api/" fallback exclusion already
	// returns this as a backend route (no fallback change needed). RequireAuth wraps the
	// whole mux below, so the new reads inherit the whole-origin gate for free.
	// Both the trailing-slash subtree (the {id} routes + /search) AND the exact
	// "/api/conversations" (the list GET, no trailing slash) are registered so the list
	// endpoint is not 301-redirected into the subtree and lost.
	mux.Handle(conversationsRoutePrefix, aguiHandler)
	mux.Handle(conversationsListRoute, aguiHandler)
	// The D-09 mutating branch re-runs (edit / branch-select) are capability-gated like
	// POST /agent/run. Their method+path-specific patterns win longest-pattern precedence
	// over the "/api/conversations/" subtree above, so the gate fires on the re-run while
	// the read GET /branches stays under RequireAuth only.
	mux.Handle(branchEditRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(branchSelectRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	// The Phase-25 approval center (APRV-01/02/03) mounts beside the conversation
	// subtree. The mutating resolve is capability-gated exactly like POST /agent/run —
	// resuming/declining/cancelling another thread's run is privileged (V4/T-25-07) —
	// while the read inherits RequireAuth from the whole-mux wrap. Both delegate to the
	// AG-UI handler, which carries the routes on its Server.Mux (registerApprovalRoutes).
	// Method+path precedence keeps the resolve gate authoritative over the read path.
	mux.Handle(approvalsResolveRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(approvalsListRoute, aguiHandler)
	// The integrations admin proxy (cockpit connect data plane) mounts ahead of the
	// "/" embed catch-all; Go 1.22 longest-pattern precedence keeps it authoritative.
	// NOTE: "/api/" is deliberately NOT registered here — it lives only in the
	// fallback exclusion set, so registering it would collide with this subtree.
	mux.Handle(integrationsRoutePrefix, newIntegrationsProxy())
	mux.Handle("/", static)
	// The login page's static assets (the shared SPA bundle/styles, PWA, icons) must be
	// reachable before a session exists so the login form can render (D-03). webui owns
	// the embedded-asset truth, so wire its predicate into the gate rather than letting
	// the auth layer guess asset paths.
	auth.PublicAsset = webui.IsPublicAsset
	// Wrap the WHOLE parent mux in the WEB-03 whole-origin gate (D-03). The public-path
	// exceptions are handled inside RequireAuth; a no-op pass-through when no secret is
	// configured keeps loopback dev unauthenticated.
	return agui.RequireAuth(mux, auth), nil
}
