package agui

import (
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/objectstore"
)

// assets_stream_api.go serves approved audio and video inline with HTTP Range, so media
// elements can start playing and seek without downloading the asset whole (R22/R24).
//
// Like /render, this route does NOT replace /api/assets/{id}/download: download stays the
// inert attachment for every asset. This one serves a real media type, which is safe only
// because the set is closed to approved media types, never
// widened by the request, and nosniff keeps the browser from reading the bytes as anything
// else. The bytes stream through the daemon; the store is never presigned or redirected to
// (D-09).

// handleAssetStream is mounted by registerAssetRoutes and inherits RequireAuth from the parent
// mux. OpenSeekableForIdentity applies download's ownership gate and heads the object, and any
// failure answers download's 404. An owned asset that is not streamable media is refused with
// 415, as /render refuses a non-HTML asset, before any byte is read.
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
	contentType, ok := mediaStreamType(w, asset.MIMEType)
	if !ok {
		return
	}
	serveMediaStream(w, r, body, contentType, asset.FileName, asset.ID)
}

// mediaStreamType strips parameters and admits only the two generated video containers or
// this closed audio set. Stored MIME, never an extension or request hint, decides the type.
func mediaStreamType(w http.ResponseWriter, mimeType string) (string, bool) {
	parsed, _, err := mime.ParseMediaType(mimeType)
	if err == nil {
		if _, err = mediagen.VideoExtension(parsed); err == nil {
			return parsed, true
		}
		switch parsed {
		case "audio/mpeg", "audio/mp3", "audio/ogg", "audio/wav", "audio/x-wav", "audio/webm", "audio/mp4", "audio/m4a", "audio/x-m4a":
			return parsed, true
		}
	}
	http.Error(w, "asset is not streamable media", http.StatusUnsupportedMediaType)
	return "", false
}

// serveMediaStream answers one stream request over an already-headed object. net/http.ServeContent
// owns the Range rules — 206 with Content-Range, 416 with bytes */size, suffix and open-ended
// ranges, If-Range, HEAD. Content-Type is set before it runs because ServeContent otherwise
// guesses from the name or sniffs the bytes.
func serveMediaStream(w http.ResponseWriter, r *http.Request, body *objectstore.SeekableObject, contentType, fileName, assetID string) {
	// Media elements do not need multipart ranges; each part would reopen the object.
	if strings.Contains(r.Header.Get("Range"), ",") {
		r.Header.Del("Range")
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", inlineContentDisposition(fileName))
	http.ServeContent(w, r, fileName, time.Time{}, body)
	// ServeContent drops a copy error once the status line is out. A cancelled request is the
	// client leaving (a seek aborts the previous request), not a store fault.
	if err := body.Err(); err != nil && r.Context().Err() == nil {
		slog.Warn("agui: media stream read failed", "asset_id", assetID, "err", err)
	}
}
