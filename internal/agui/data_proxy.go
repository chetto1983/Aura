package agui

import (
	"context"
	"net/http"
	"strconv"
)

// DataFetcher is the narrow SSRF-safe data-fetch surface the proxy consumes
// (*web.Client satisfies it via FetchData). Declared consumer-side (D-A2-02) so the
// agui server depends only on the one method it calls, and so a unit test can pass a
// scripted fake. The returned (bytes, mediaType) is already gated by the web SSRF
// guard + data content-type allowlist + size cap; an error is a sanitized *web.WebError.
type DataFetcher interface {
	FetchData(ctx context.Context, convID, rawURL string) ([]byte, string, error)
}

// dataProxyConvID scopes the per-request DNS pin for proxied data fetches, mirroring
// imageProxyConvID: the pin's job here is anti-rebinding within the request, not
// per-conversation isolation as in web_fetch.
const dataProxyConvID = "data-proxy"

// handleDataProxy serves GET /api/fetch?url=<external data url>. It is the general
// case of handleImageProxy — same auth gate, same SSRF-safe fetch, a different
// content allowlist — so a new data source costs a URL rather than a new endpoint.
//
// It is mounted behind the Phase-24 RequireAuth whole-origin gate inherited by every
// /api/ route, so it is never an open relay. Note what that gate implies about the
// caller: a SEALED ARTIFACT CANNOT REACH THIS ROUTE. Artifact previews are served
// from Aura's own origin under a CSP `sandbox` directive, so they hold an OPAQUE origin:
// a call to /api/fetch from inside one is cross-origin and carries no session, and is
// refused like any other anonymous request. This proxy therefore serves the cockpit, not
// the sandbox. A preview reaches external APIs directly — its connect-src is open — and
// succeeds wherever that API sends CORS headers.
func (s *Server) handleDataProxy(w http.ResponseWriter, r *http.Request) {
	if s.data == nil {
		http.Error(w, "data proxy not configured", http.StatusServiceUnavailable)
		return
	}
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing url query parameter", http.StatusBadRequest)
		return
	}
	body, mediaType, err := s.data.FetchData(r.Context(), dataProxyConvID, rawURL)
	if err != nil {
		// Already a sanitized WebError (no IP/host/redirect leak); SanitizeString is the
		// belt-and-suspenders applied to every wire string.
		http.Error(w, SanitizeString(err.Error()), http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", mediaType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	// Live data, unlike a thumbnail, is stale the moment it is stored — and the reply
	// is identity-scoped, so no shared intermediary may key it on the URL alone.
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body) //nolint:gosec // G705: FetchData already enforces the data content-type allowlist, SSRF checks, and size caps.
}
