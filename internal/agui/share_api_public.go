// share_api_public.go owns the UNAUTHENTICATED public token routes (WEBSHARE-02/03, plan
// 37F-10): GET /s/{token}/data, GET /s/{token}/asset/{id} and its video stream sibling
// GET /s/{token}/asset/{id}/stream. These are the phase's ONLY
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
	"net/http"

	"github.com/chetto1983/aura/internal/share"
)

// registerSharePublicRoutes mounts the unauthenticated token routes. The token predicate
// (share.Service.ResolveByToken) is the entire gate — there is no per-route auth wiring here
// and none is added at the parent mux either (plan 37F-12).
func (s *Server) registerSharePublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /s/{token}/data", s.handleShareResolvePublic)
	mux.HandleFunc("GET /s/{token}/asset/{id}", s.handleShareAssetPublic)
	mux.HandleFunc("GET /s/{token}/asset/{id}/stream", s.handleShareAssetStreamPublic)
}

// handleShareResolvePublic resolves a live public link with NO session (D-15's lazy
// fail-closed predicate — see share.Store.ResolveByToken's own doc).
func (s *Server) handleShareResolvePublic(w http.ResponseWriter, r *http.Request) {
	snap, _, ok := s.resolvePublicShare(w, r)
	if !ok {
		return
	}
	writeJSON(w, snap)
}

// handleShareAssetPublic serves one bundled artifact of the resolved public snapshot as an inert
// attachment (share_api_artifact.go).
func (s *Server) handleShareAssetPublic(w http.ResponseWriter, r *http.Request) {
	snap, link, ok := s.resolvePublicShare(w, r)
	if !ok {
		return
	}
	s.serveShareArtifact(w, r, snap, link, r.PathValue("id"))
}

// handleShareAssetStreamPublic is the Range-capable inline sibling for a bundled video — the
// token-scoped equivalent of /api/assets/{id}/stream.
func (s *Server) handleShareAssetStreamPublic(w http.ResponseWriter, r *http.Request) {
	snap, link, ok := s.resolvePublicShare(w, r)
	if !ok {
		return
	}
	s.streamShareArtifact(w, r, snap, link, r.PathValue("id"))
}

// resolvePublicShare passes the token to the resolver UNCHECKED: no length/shape early-return
// before the DB probe (a timing oracle), and ANY failure collapses to the SAME 404 body.
func (s *Server) resolvePublicShare(w http.ResponseWriter, r *http.Request) (share.Snapshot, share.Link, bool) {
	if s.share == nil {
		http.Error(w, "share service unavailable", http.StatusServiceUnavailable)
		return share.Snapshot{}, share.Link{}, false
	}
	snap, link, err := s.share.ResolveByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return share.Snapshot{}, share.Link{}, false
	}
	return snap, link, true
}
