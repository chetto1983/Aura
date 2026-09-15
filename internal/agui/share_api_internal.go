// share_api_internal.go owns the D-10 BEARER-WITHIN-AUTH internal share routes
// (WEBSHARE-02/03, plan 37F-10): GET /api/shares/{id}/data, GET /api/shares/{id}/asset/{assetID}
// and its video stream sibling. These are the ONLY routes in the phase where a
// non-owner's read succeeds BY DESIGN — RequireAuth (the parent-mux mount, plan 37F-12) is the
// gate and the unguessable share id is the capability; the already-redacted snapshot (D-08)
// bounds what a bearer can see. No handler here runs an owner predicate, and that omission is
// D-10-intended, not a bug — it is the OPPOSITE rule from share_api.go's owner-scoped CRUD
// handlers in the SAME package, which is exactly why the two live in separate files: a reader
// who greps one and generalises to the other gets one of them wrong.
//
// Tier and liveness are resolved ENTIRELY by share.Service.ResolveInternal's SQL predicate
// (tier='internal' AND live) — there is NO Go-side tier check anywhere in this file. A
// public-tier id, a revoked link, an expired link, and an unknown id are therefore all the
// SAME miss: one shared 404, never a distinguishable status (never 403, never 409) that would
// confirm the id names something real.
package agui

import (
	"net/http"

	"github.com/chetto1983/aura/internal/share"
)

// registerShareInternalRoutes mounts the D-10 bearer-within-auth routes. RequireAuth ONLY at the
// mount (plan 37F-12) — no capability, no owner predicate.
func (s *Server) registerShareInternalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/shares/{id}/data", s.handleShareResolveInternal)
	mux.HandleFunc("GET /api/shares/{id}/asset/{assetID}", s.handleShareAssetInternal)
	mux.HandleFunc("GET /api/shares/{id}/asset/{assetID}/stream", s.handleShareAssetStreamInternal)
}

// handleShareResolveInternal is the D-10 route the whole internal tier hangs on:
// share.Service exposes ResolveInternal and NOTHING else calls it. It carries NO capability
// (D-02) and — deliberately — NO owner predicate: any authenticated identity holding shareID
// resolves its already-redacted snapshot, D-10 by design.
func (s *Server) handleShareResolveInternal(w http.ResponseWriter, r *http.Request) {
	snap, _, ok := s.resolveInternalShare(w, r)
	if !ok {
		return
	}
	writeJSON(w, snap)
}

// handleShareAssetInternal serves one bundled artifact from a bearer's resolved snapshot (D-09:
// internal shares resolve artifacts via the SAME copy-never-reference bundle as the public
// tier). Like handleShareResolveInternal above, it carries NO owner predicate — D-10 by design,
// not an omission. The bearer is not the owner, so the identity-scoped artifact API would 404
// anyway; share_api_artifact.go reads the token-scoped share/ namespace instead.
func (s *Server) handleShareAssetInternal(w http.ResponseWriter, r *http.Request) {
	snap, link, ok := s.resolveInternalShare(w, r)
	if !ok {
		return
	}
	s.serveShareArtifact(w, r, snap, link, r.PathValue("assetID"))
}

// handleShareAssetStreamInternal is the Range-capable inline sibling for a bundled video, under
// the same D-10 bearer rule.
func (s *Server) handleShareAssetStreamInternal(w http.ResponseWriter, r *http.Request) {
	snap, link, ok := s.resolveInternalShare(w, r)
	if !ok {
		return
	}
	s.streamShareArtifact(w, r, snap, link, r.PathValue("assetID"))
}

// resolveInternalShare passes the caller's identity to ResolveInternal ONLY for the service's
// audit trail, never as a filter; every miss is the one shared 404.
func (s *Server) resolveInternalShare(w http.ResponseWriter, r *http.Request) (share.Snapshot, share.Link, bool) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return share.Snapshot{}, share.Link{}, false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return share.Snapshot{}, share.Link{}, false
	}
	snap, link, err := s.share.ResolveInternal(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return share.Snapshot{}, share.Link{}, false
	}
	return snap, link, true
}
