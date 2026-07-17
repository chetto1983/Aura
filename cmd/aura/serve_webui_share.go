package main

// serve_webui_share.go carries the 37F WEBSHARE-02 share-lifecycle parent-mux mounts, kept
// OUT of serve_webui.go so that file stays under the 600-LOC ceiling (mirrors the
// serve_webui_musr.go / serve_webui_voice.go / serve_webui_composer.go extractions). It
// mounts the eight share routes registered on the agui Server.Mux (share_api.go /
// share_api_internal.go / share_api_public.go):
//
//   - POST /api/shares — bare aguiHandler, RequireAuth-only. D-02: internal links need NO
//     capability; any owner shares their own thread. A single mux entry answers BOTH the
//     internal and the public tier (the tier lives in the JSON body, not the route), so a
//     tier-specific gate cannot live at the mount — Go's ServeMux dispatches on method+path
//     only, never on request body. The public tier's org kill-switch
//     (AURA_SHARE_PUBLIC_ENABLED) is re-checked in-handler (share_api.go), the gate that
//     still holds when RequireCapability would otherwise vanish on loopback (auth.go:282,
//     R-08).
//   - GET /api/shares, PATCH /api/shares/{id}/snapshot, DELETE /api/shares/{id} — bare
//     aguiHandler; owner-scoped, *ForIdentity-gated, 404-on-foreign (D-06 — a foreign id and
//     an absent id must be indistinguishable).
//   - GET /api/shares/{id}/data, GET /api/shares/{id}/asset/{assetID} — bare aguiHandler,
//     RequireAuth-only, and deliberately NOT admitted to PublicRoute below. This is the D-10
//     bearer-within-auth tier (plan 37F-10): TWO halves, both load-bearing. Half one — NO
//     capability and NO owner predicate: any authenticated identity holding the share id
//     resolves its already-redacted snapshot, which is the entire point of an "internal
//     link". Half two — authenticated: the routes sit on the authenticated /api/ lane so
//     RequireAuth gates them for free; an anonymous caller gets 401/302. Do NOT "unify"
//     these onto the /s/ lane below — isPublicShareRoute admits every GET /s/... with no
//     session at all, so moving them there would make every internal share world-readable.
//   - GET /s/{token}/data, GET /s/{token}/asset/{id} — bare aguiHandler PLUS the
//     PublicRoute admission wired in serve_webui.go. These are the phase's ONLY
//     unauthenticated routes in the whole binary; the opaque token itself is their entire
//     gate.
//
// Method+path-specific patterns win Go 1.22 longest-pattern precedence over the bare "/api/"
// exclusion carve-out and the "/" embed catch-all (mirrors _musr.go:47-50); the "/api/"
// fallback exclusion (serve_webui.go) already returns an unmatched /api/shares/... path as a
// backend 404 — nothing to add there. The /s/ routes carry NO fallback-exclusion entry at
// all (see serve_webui.go's deliberate non-action) because they must fall THROUGH to the SPA
// embed, not be excluded from it.
//
// KNOWN GAP (logged, not fixed here — see deferred-items.md): sharePublicCapability below is
// a documented D-02 constant, but no code path currently calls
// Identities.HasCapability(identityID, sharePublicCapability) — share_api.go's
// handleShareCreate re-checks only the org kill-switch (s.cfg.SharePublicEnabled), never the
// per-user capability. That check cannot live at THIS mount either, for the same
// single-route/dual-tier reason above. Closing it means editing internal/agui/share_api.go,
// which is outside this plan's declared file scope.

import (
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
)

// sharePublicCapability is the D-02 capability_grants name for the public-share tier:
// per-user grantable, off by default, admin-grantable — exactly identity.create's shape
// (serve_webui.go:271-278's identityCreateCapability comment is the precedent this doc
// mirrors), NOT governance.write's (:110-118). It is deliberately NOT a reuse of
// governance.write: that would mean "to share your own chat publicly you must be a full org
// admin who can install MCP servers and RISKY supply-chain skills" — a privilege-escalation
// smell that contradicts D-02's per-user semantics. share.public is identity.create's
// sibling, not governance.write's.
//
// Verified at plan time (37F-RESEARCH.md A5/R-13): the bootstrap identity is granted the
// literal "*" wildcard (serve_bootstrap.go:176-180), so the operator auto-holds
// share.public — intended, they are the admin — while provisioned identities receive only
// the explicit named capabilities their creator grants (serve_onboarding.go:153-165), never
// the wildcard and never share.public unless a holder explicitly grants it. That contrast is
// what makes the per-user, off-by-default semantics real rather than vacuous.
const sharePublicCapability = "share.public"

// The eight WEBSHARE-02/03 share-lifecycle routes. share_api.go / share_api_internal.go /
// share_api_public.go own the handlers (registerShareRoutes on *agui.Server); this file only
// mounts the parent-mux entries that delegate to them.
const (
	shareCreateRoute         = "POST /api/shares"
	shareListRoute           = "GET /api/shares"
	shareSnapshotUpdateRoute = "PATCH /api/shares/{id}/snapshot"
	shareRevokeRoute         = "DELETE /api/shares/{id}"
	// shareInternalDataRoute / shareInternalAssetRoute are the D-10 bearer-within-auth
	// routes: bare aguiHandler, RequireAuth-only, deliberately absent from
	// isPublicShareRoute below — see the file header's two-halves note.
	shareInternalDataRoute  = "GET /api/shares/{id}/data"
	shareInternalAssetRoute = "GET /api/shares/{id}/asset/{assetID}"
	// sharePublicDataRoute / sharePublicAssetRoute are the phase's ONLY unauthenticated
	// routes — admitted by isPublicShareRoute below AND the PublicRoute chain entry added
	// in serve_webui.go.
	sharePublicDataRoute  = "GET /s/{token}/data"
	sharePublicAssetRoute = "GET /s/{token}/asset/{id}"
)

// isPublicShareRoute is the fail-closed /s/ allowlist predicate serve_webui.go's PublicRoute
// chain admits unauthenticated (T-37F-57). Unlike isPublicPasswordResetRoute's exact-path
// switch, this needs a PREFIX match — /s/{token}, /s/{token}/data, and /s/{token}/asset/{id}
// all share the "/s/" prefix. GET-only; every other method and every other path (including
// the confusable "/shared/..." internal-tier page — "/sh" != "/s/" — and a naive bare "/s"
// with no trailing slash) returns false by default.
func isPublicShareRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/s/")
}

// registerShareRoutes mounts the WEBSHARE-02/03 share-lifecycle routes on the parent mux.
// Every route below is a bare aguiHandler — see the file header for why the internal tier
// carries no capability gate and the D-10 tier carries no owner predicate. auth is accepted
// for signature parity with registerVoiceRoutes/registerMUSRRoutes/registerComposerRoutes and
// any future gated share route.
func registerShareRoutes(mux *http.ServeMux, aguiHandler http.Handler, _ agui.AuthDeps) {
	mux.Handle(shareCreateRoute, aguiHandler)
	mux.Handle(shareListRoute, aguiHandler)
	mux.Handle(shareSnapshotUpdateRoute, aguiHandler)
	mux.Handle(shareRevokeRoute, aguiHandler)
	// D-10 bearer-within-auth: bare aguiHandler, NO RequireCapability, NO owner predicate —
	// authenticated ONLY because RequireAuth wraps the whole parent mux (serve_webui.go).
	// Do not move these onto the /s/ lane; see the file header's two-halves note.
	mux.Handle(shareInternalDataRoute, aguiHandler)
	mux.Handle(shareInternalAssetRoute, aguiHandler)
	// The phase's ONLY unauthenticated routes; the PublicRoute admission itself lives in
	// serve_webui.go, not here.
	mux.Handle(sharePublicDataRoute, aguiHandler)
	mux.Handle(sharePublicAssetRoute, aguiHandler)
}
