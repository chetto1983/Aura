package agui

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/assets"
)

func (s *Server) registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/assets/presign", s.handleAssetPresign)
	mux.HandleFunc("POST /api/assets/{id}/finalize", s.handleAssetFinalize)
	mux.HandleFunc("GET /api/assets/{id}", s.handleAssetGet)
	mux.HandleFunc("GET /api/assets/{id}/download", s.handleAssetDownload)
	mux.HandleFunc("GET /api/assets", s.handleAssetList)
	mux.HandleFunc("POST /api/assets/{id}/promote", s.handleAssetPromote)
	mux.HandleFunc("POST /api/assets/{id}/retry", s.handleAssetRetry)
	mux.HandleFunc("DELETE /api/assets/{id}", s.handleAssetDelete)
}

// handleAssetDownload streams the owner's asset body from the object store as a forced,
// stored-XSS-safe attachment (WEBART-03). It inherits RequireAuth whole-origin from the parent
// mux (no per-route auth wiring, no unauthenticated surface). The security invariants:
//   - D-12 existence-hiding: OpenForIdentity's ownership gate precedes any store read, and ANY
//     error (not-found OR not-owned) collapses to 404 — never 403, never 200.
//   - D-10 stored-XSS guard: the serve Content-Type is the neutral application/octet-stream plus
//     X-Content-Type-Options: nosniff regardless of the sniffed asset.MIMEType, which is NEVER
//     trusted as a serve header.
//   - D-11 header-injection guard: the filename rides Content-Disposition via contentDisposition.
//   - D-09 DoS-safe stream-through: the read is scoped to r.Context() so a client disconnect
//     cancels it and io.Copy unblocks (no goroutine leak); the stream is never presigned/redirected.
func (s *Server) handleAssetDownload(w http.ResponseWriter, r *http.Request) {
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

	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", contentDisposition(asset.FileName))
	h.Set("Content-Length", strconv.FormatInt(asset.SizeBytes, 10))

	_, _ = io.Copy(w, rc)
}

type assetPresignBody struct {
	ThreadID  string `json:"thread_id"`
	Scope     string `json:"scope"`
	FileName  string `json:"file_name"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Modality  string `json:"modality_hint"`
}

func (s *Server) handleAssetPresign(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := decodeAssetPresignBody(w, r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	resp, err := s.assets.Presign(r.Context(), assets.PresignRequest{
		IdentityID:        identityID,
		SourceKind:        assets.SourceWeb,
		ThreadID:          body.ThreadID,
		Scope:             assets.Scope(body.Scope),
		FileName:          body.FileName,
		MIMEType:          body.MIMEType,
		DeclaredSizeBytes: body.SizeBytes,
		ModalityHint:      assets.Modality(body.Modality),
	})
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	writeJSON(w, resp)
}

func decodeAssetPresignBody(w http.ResponseWriter, r *http.Request) (assetPresignBody, error) {
	var body assetPresignBody
	err := strictDecodeJSON(w, r, &body, decodeOpts{})
	return body, err
}

func (s *Server) handleAssetFinalize(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.callAssetMutation(w, r, func(ctx context.Context, identityID, id string) (assets.Asset, error) {
		return s.assets.Finalize(ctx, identityID, id)
	})
	if !ok {
		return
	}
	writeJSON(w, asset)
}

func (s *Server) handleAssetPromote(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.callAssetMutation(w, r, func(ctx context.Context, identityID, id string) (assets.Asset, error) {
		return s.assets.Promote(ctx, identityID, id)
	})
	if !ok {
		return
	}
	writeJSON(w, asset)
}

func (s *Server) handleAssetRetry(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.callAssetMutation(w, r, func(ctx context.Context, identityID, id string) (assets.Asset, error) {
		return s.assets.Retry(ctx, identityID, id)
	})
	if !ok {
		return
	}
	writeJSON(w, asset)
}

func (s *Server) handleAssetDelete(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.callAssetMutation(w, r, func(ctx context.Context, identityID, id string) (assets.Asset, error) {
		return s.assets.Delete(ctx, identityID, id)
	})
	if !ok {
		return
	}
	writeJSON(w, asset)
}

func (s *Server) handleAssetGet(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	asset, err := s.assets.GetForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	writeJSON(w, asset)
}

func (s *Server) handleAssetList(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := s.assets.ListForThread(r.Context(), identityID, r.URL.Query().Get("thread_id"))
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *Server) callAssetMutation(
	w http.ResponseWriter,
	r *http.Request,
	call func(context.Context, string, string) (assets.Asset, error),
) (assets.Asset, bool) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return assets.Asset{}, false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return assets.Asset{}, false
	}
	asset, err := call(r.Context(), identityID, r.PathValue("id"))
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return assets.Asset{}, false
	}
	return asset, true
}

func principalIdentityID(r *http.Request) (string, bool) {
	identityID := principalFrom(r.Context())
	return identityID, identityID != ""
}
