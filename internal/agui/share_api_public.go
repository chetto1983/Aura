// share_api_public.go owns the UNAUTHENTICATED public token routes (WEBSHARE-02/03, plan
// 37F-10): GET /s/{token}/data and GET /s/{token}/asset/{id}. These are the phase's ONLY
// unauthenticated handlers — INVERTING assets_api.go's handleAssetDownload doc line ("no
// unauthenticated surface"): that line is FALSE here. RequireAuth does NOT apply to this
// prefix; the token predicate itself is the entire gate (plan 37F-12's isPublicShareRoute
// allowlist admits /s/... unauthenticated at the parent mux — none of that wiring is in this
// file).
//
// ANY resolve failure — an unknown token, an expired link, a revoked link, or a malformed
// value — returns the SAME 404 status and the SAME literal body. Distinguishing them would be
// an oracle: "expired" confirms the token WAS valid once. There is no length/shape check on
// the token before the store probe either — a structural fast-path on a route that otherwise
// does a DB read is a timing oracle (T-37F-51).
package agui

import (
	"io"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/share"
)

// registerSharePublicRoutes mounts the two unauthenticated token routes. The token predicate
// (share.Service.ResolveByToken) is the entire gate — there is no per-route auth wiring here
// and none is added at the parent mux either (plan 37F-12).
func (s *Server) registerSharePublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /s/{token}/data", s.handleShareResolvePublic)
	mux.HandleFunc("GET /s/{token}/asset/{id}", s.handleShareAssetPublic)
}

// handleShareResolvePublic resolves a live public link with NO session (D-15's lazy
// fail-closed predicate — see share.Store.ResolveByToken's own doc). The token is passed to
// the resolver UNCHECKED: no length/shape early-return before the DB probe (a timing oracle),
// and ANY failure collapses to the SAME 404 body below.
func (s *Server) handleShareResolvePublic(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	snap, _, err := s.share.ResolveByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snap)
}

// handleShareAssetPublic streams one bundled artifact from a resolved public snapshot. The
// token is resolved FIRST; id must belong to THAT snapshot (SC4 row 9) or the response is
// 404 — holding a token authenticates one snapshot, never any asset id. Bytes are read only
// from the token-scoped share/ object-store namespace, never through the identity-scoped
// artifact API (D-09 copy-never-reference).
func (s *Server) handleShareAssetPublic(w http.ResponseWriter, r *http.Request) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return
	}
	snap, link, err := s.share.ResolveByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	assetID := r.PathValue("id")
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
	// serve header — application/octet-stream + nosniff regardless, mirroring
	// handleAssetDownload (assets_api.go) verbatim.
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition(artifact.FileName))
	h.Set("Content-Length", strconv.FormatInt(artifact.SizeBytes, 10))
	_, _ = io.Copy(w, rc)
}
