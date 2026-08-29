package agui

import (
	"io"
	"mime"
	"net/http"
	"strings"
)

// assets_render_api.go serves an agent-delivered HTML artifact as a real document, so
// the cockpit can frame it with src= instead of srcdoc.
//
// The distinction is the whole point of the route. A srcdoc frame inherits the
// embedder's Content-Security-Policy and cannot be given one of its own, which left the
// artifacts panel in a bad spot in both directions: the cockpit could not tighten its
// own policy without blanking every artifact, and the artifact could not be confined by
// a policy of its own. A document fetched from a URL carries the headers of its
// response, and nothing of the embedder's, so both halves become possible at once.
//
// This route does NOT replace /api/assets/{id}/download. Download stays the neutral
// forced attachment for every asset (application/octet-stream + nosniff, D-10). This
// one deliberately serves text/html — which is only safe because the response is
// scrubbed, policy-bearing, and framed WITHOUT the same-origin token, so the document
// lands in an opaque origin holding no session.

// maxRenderableArtifactBytes bounds what this route will parse into memory. The whole
// document must be resident to be parsed and re-rendered, so an unbounded artifact is
// an unbounded allocation on an authenticated route. A self-contained page with an
// inlined React bundle and its CSS runs a few hundred KB; 8 MiB leaves room for one
// carrying embedded images without letting the route become a memory lever.
const maxRenderableArtifactBytes = 8 << 20

// handleAssetRender streams the owner's HTML artifact as a sealed document. It is
// mounted by registerAssetRoutes (assets_api.go) beside its sibling asset routes and
// inherits RequireAuth from the parent mux — there is no unauthenticated path here.
//
// Invariants, in the order they are enforced:
//   - ownership first: OpenForIdentity gates on the caller's identity before any read,
//     and not-found and not-owned collapse to the same 404 (D-12 existence hiding).
//   - HTML only: an asset that is not HTML is refused rather than served as HTML. The
//     renderable set is decided HERE, from the stored MIME and file name, and never
//     from anything the request supplies.
//   - bounded: the body is read through a limit, and a document that exceeds it is
//     refused instead of being silently truncated into malformed markup.
//   - sealed: the response carries the sealed-view CSP, nosniff, and no filename — the
//     bytes are for framing, not for saving; download remains the route that saves.
func (s *Server) handleAssetRender(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rc, asset, err := s.assets.OpenForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	defer func() { _ = rc.Close() }()

	if !renderableArtifact(asset.MIMEType, asset.FileName) {
		http.Error(w, "asset is not a renderable HTML artifact", http.StatusUnsupportedMediaType)
		return
	}

	// One byte past the cap distinguishes "exactly at the cap" from "over it", so a
	// document that would be truncated is refused instead of rendered malformed.
	raw, err := io.ReadAll(io.LimitReader(rc, maxRenderableArtifactBytes+1))
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadGateway)
		return
	}
	if len(raw) > maxRenderableArtifactBytes {
		http.Error(w, "artifact too large to render", http.StatusRequestEntityTooLarge)
		return
	}

	sealed, err := prepareArtifactHTML(string(raw))
	if err != nil {
		http.Error(w, "artifact could not be prepared for rendering", http.StatusUnprocessableEntity)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", artifactRenderCSP())
	// The sealed bytes are derived per request and must not be cached by a shared
	// intermediary keyed on the URL alone — the URL is identity-scoped, the cache is not.
	h.Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, sealed)
}

// renderableArtifact reports whether an owned asset may be served as a document.
//
// The stored MIME is the primary signal and the file name is the fallback, because an
// agent-delivered file can arrive with a generic octet-stream type while plainly being
// a page. Both are matched narrowly: this route exists to render pages the agent made,
// not to become a general viewer for whatever an identity happens to own. Anything
// script-bearing but not HTML (an .svg above all) is deliberately outside the set — it
// has its own reasons to stay out of an executing context (T-37B-05).
func renderableArtifact(mimeType, fileName string) bool {
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		if strings.EqualFold(strings.TrimSpace(parsed), "text/html") {
			return true
		}
	}
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm")
}
