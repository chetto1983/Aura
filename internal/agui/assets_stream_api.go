package agui

import (
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/mediagen"
)

// assets_stream_api.go serves a video asset inline with HTTP Range, so the cockpit's <video>
// element can start playing and seek without downloading the clip whole (R22/R24).
//
// Like /render, this route does NOT replace /api/assets/{id}/download: download stays the
// inert attachment for every asset. This one serves a real media type, which is safe only
// because the set is closed to the two video containers the asset pipeline accepts, never
// widened by the request, and nosniff keeps the browser from reading the bytes as anything
// else. The bytes stream through the daemon; the store is never presigned or redirected to
// (D-09).

// handleAssetStream is mounted by registerAssetRoutes and inherits RequireAuth from the parent
// mux. OpenSeekableForIdentity applies download's ownership gate, and a miss answers download's
// 404. An owned asset that is not a streamable video is refused with 415, as /render refuses a
// non-HTML asset; the store has not been opened by then.
func (s *Server) handleAssetStream(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.assetCaller(w, r)
	if !ok {
		return
	}
	body, asset, err := s.assets.OpenSeekableForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)
		return
	}
	defer func() { _ = body.Close() }()
	serveVideoStream(w, r, body, asset.MIMEType, asset.FileName)
}

// serveVideoStream answers one stream request over body. net/http.ServeContent owns every Range
// rule — 206 with Content-Range, 416 with bytes */size, suffix and open-ended ranges, If-Range,
// multipart byteranges, HEAD. Content-Type is set before it runs because ServeContent otherwise
// guesses from the name or sniffs the bytes.
func serveVideoStream(w http.ResponseWriter, r *http.Request, body io.ReadSeeker, mimeType, fileName string) {
	contentType, ok := streamableVideoType(mimeType)
	if !ok {
		http.Error(w, "asset is not a streamable video", http.StatusUnsupportedMediaType)
		return
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", inlineContentDisposition(fileName))
	http.ServeContent(w, r, fileName, time.Time{}, body)
}

// streamableVideoType returns the bare media type of an MP4 or WebM MIME, the only video the
// asset pipeline stores (mediagen.VideoExtension), with any parameters dropped.
func streamableVideoType(mimeType string) (string, bool) {
	parsed, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "", false
	}
	if _, err := mediagen.VideoExtension(parsed); err != nil {
		return "", false
	}
	return parsed, true
}
