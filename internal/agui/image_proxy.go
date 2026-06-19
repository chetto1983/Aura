package agui

import (
	"context"
	"net/http"
	"strconv"
)

// ImageFetcher is the narrow SSRF-safe image-fetch surface the proxy consumes
// (*web.Client satisfies it via FetchImage). Declared consumer-side (D-A2-02) so the
// agui server depends only on the one method it calls, not the whole web.Client — and
// so a unit test can pass a scripted fake. The returned (bytes, mediaType) is already
// gated by the web SSRF guard + image content-type allowlist + size cap; an error is a
// sanitized *web.WebError (no IP/host leak).
type ImageFetcher interface {
	FetchImage(ctx context.Context, convID, rawURL string) ([]byte, string, error)
}

// imageProxyConvID scopes the per-request DNS pin for proxied image fetches. The
// cockpit is a single whole-origin-private operator, so a fixed scope is sufficient
// (the pin's job here is anti-rebinding within the request, not per-conversation
// isolation as in web_fetch).
const imageProxyConvID = "image-proxy"

// imageProxyMaxCacheAge is the browser Cache-Control max-age for a successfully proxied
// image (1 hour): thumbnails/favicons are effectively immutable per turn, and caching
// them avoids re-dialing the SSRF guard on every re-render.
const imageProxyMaxCacheAge = 3600

// handleImageProxy serves GET /api/image-proxy?url=<external image url> (D-09). It is
// mounted behind the Phase-24 RequireAuth whole-origin gate (inherited by every /api/
// route), so it is never an open relay. It reads the url query param, delegates to the
// SSRF-safe FetchImage (hostname blocklist → DNS-pin → classify → image allowlist →
// size cap → no redirect follow), and streams the bytes back with a strict
// Content-Type + Cache-Control. A missing fetcher (unwired) is 503; a bad/blocked URL
// is 400 with a sanitized reason (no SSRF internals).
func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	if s.images == nil {
		http.Error(w, "image proxy not configured", http.StatusServiceUnavailable)
		return
	}
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing url query parameter", http.StatusBadRequest)
		return
	}
	body, mediaType, err := s.images.FetchImage(r.Context(), imageProxyConvID, rawURL)
	if err != nil {
		// The fetch error is already a sanitized WebError (no IP/host/redirect leak);
		// SanitizeString is the belt-and-suspenders applied to every wire string.
		http.Error(w, SanitizeString(err.Error()), http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", mediaType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "private, max-age="+strconv.Itoa(imageProxyMaxCacheAge))
	// Defense-in-depth: the proxied bytes are an image, never a document; forbid the
	// browser from sniffing them into an executable type.
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body) //nolint:gosec // G705: FetchImage already enforces image content-type, SSRF checks, and size caps.
}
