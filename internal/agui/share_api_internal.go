// share_api_internal.go owns the D-10 BEARER-WITHIN-AUTH internal share routes
// (WEBSHARE-02/03, plan 37F-10): GET /api/shares/{id}/data and
// GET /api/shares/{id}/asset/{assetID}. This is the ONLY pair of routes in the phase where a
// non-owner's read succeeds BY DESIGN — RequireAuth (the parent-mux mount, plan 37F-12) is the
// gate and the unguessable share id is the capability; the already-redacted snapshot (D-08)
// bounds what a bearer can see. Neither handler runs an owner predicate, and that omission is
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
	"io"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/share"
)

// registerShareInternalRoutes mounts the two D-10 bearer-within-auth routes. RequireAuth ONLY
// at the mount (plan 37F-12) — no capability, no owner predicate.
func (s *Server) registerShareInternalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/shares/{id}/data", s.handleShareResolveInternal)
	mux.HandleFunc("GET /api/shares/{id}/asset/{assetID}", s.handleShareAssetInternal)
}

// handleShareResolveInternal is the D-10 route the whole internal tier hangs on:
// share.Service exposes ResolveInternal and NOTHING else calls it. It carries NO capability
// (D-02) and — deliberately — NO owner predicate: any authenticated identity holding shareID
// resolves its already-redacted snapshot, D-10 by design. identityID is passed only for the
// service's audit trail, never as a filter.
func (s *Server) handleShareResolveInternal(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	snap, _, err := s.share.ResolveInternal(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snap)
}

// handleShareAssetInternal serves one bundled artifact from a bearer's resolved snapshot
// (D-09: internal shares resolve artifacts via the SAME copy-never-reference bundle as the
// public tier). Like handleShareResolveInternal above, it carries NO owner predicate — D-10 by
// design, not an omission. The share is resolved FIRST; assetID must belong to THAT snapshot
// (SC4 row 9) or the response is 404 — a link authenticates one snapshot, never any asset id.
// Bytes are read only from the token-scoped share/ object-store namespace, never through the
// identity-scoped artifact API (D-09 copy-never-reference) — the bearer is not the owner, so
// that lane would 404 anyway.
func (s *Server) handleShareAssetInternal(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	snap, link, err := s.share.ResolveInternal(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	assetID := r.PathValue("assetID")
	var artifact share.SnapshotArtifact
	found := false
	for _, a := range snap.Artifacts {
		if a.AssetID == assetID {
			artifact, found = a, true
			break
		}
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rc, err := s.share.OpenArtifact(r.Context(), link.ID, link.SnapshotID, assetID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer func() { _ = rc.Close() }()

	// Neutral-typed attachment (37A D-10): the artifact's real MIME is NEVER trusted as a
	// serve header — application/octet-stream + nosniff regardless, the same inert-bytes rule
	// handleAssetDownload (assets_api.go) applies; a bearer is still a recipient, not a
	// public-tier special case.
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition(artifact.FileName))
	h.Set("Content-Length", strconv.FormatInt(artifact.SizeBytes, 10))
	_, _ = io.Copy(w, rc)
}
