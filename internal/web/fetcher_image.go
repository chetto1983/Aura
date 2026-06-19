package web

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// allowedImageContentTypes is the image MIME allowlist the proxy gates BEFORE
// streaming the body (D-09). image/svg+xml is deliberately EXCLUDED (A5): an SVG can
// carry inline <script>, so serving one through a same-origin proxy would re-open an
// XSS vector the cockpit forbids. Matched on the media type only (charset/params
// stripped), like contentTypeAllowed for HTML.
var allowedImageContentTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
}

// FetchImage fetches an external image (a web_result thumbnail/favicon) through the
// SAME SSRF defense web_fetch uses — NOT a fresh http.Get, which would re-open the
// hole D-09 forbids. It runs: parse + scheme allowlist (D-15) → the hardened
// transport's dialContext (validateAndPin: hostname blocklist → DNS resolve →
// classify-every → pin → dial only the pinned IP, transport.go) → reject any 3xx
// (CheckRedirect never auto-follows, so a redirect is surfaced and rejected here,
// never silently chased to a private target) → image content-type allowlist (svg
// excluded) → size cap via io.LimitReader. It returns the raw bytes + the matched
// media type. Every failure is a sanitized *WebError carrying NO IP/host/redirect
// detail (D-26/27/28), mirroring Fetch.
func (c *Client) FetchImage(ctx context.Context, convID, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return nil, "", &WebError{Code: CodeHTTPError, Reason: ReasonInvalidTarget, Message: "could not parse URL"}
	}
	if _, ok := allowedSchemes[strings.ToLower(u.Scheme)]; !ok {
		return nil, "", &WebError{Code: CodeUnsupportedScheme, Message: "only http and https are supported"}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.WebFetchTimeoutSec)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(withConvID(ctx, convID), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", &WebError{Code: CodeHTTPError, Message: "could not build request"}
	}
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.transport.client.Do(req)
	if err != nil {
		// classifyTransportErr maps an SSRF block (the dialContext rejected the pinned
		// IP) to a non-retryable internalError; classifyFetchErr then sanitizes it to a
		// model/UI-safe WebError (no IP/host leak).
		return nil, "", c.classifyFetchErr(classifyTransportErr(err))
	}
	defer func() { _ = resp.Body.Close() }()

	// A 3xx is NOT auto-followed (CheckRedirect = ErrUseLastResponse). The proxy does
	// not chase redirects for an image — a redirect target could rebind to a private
	// host between hops, so it is rejected outright rather than re-validated.
	if isRedirect(resp.StatusCode) {
		return nil, "", &WebError{Code: CodeHTTPError, Reason: "redirect_not_followed", Message: "image redirect not followed"}
	}
	if resp.StatusCode/100 != 2 {
		return nil, "", &WebError{Code: CodeHTTPError, Message: "non-success status", StatusCode: resp.StatusCode}
	}

	mediaType, ok := imageContentType(resp.Header.Get("Content-Type"))
	if !ok {
		return nil, "", &WebError{Code: CodeUnsupportedContent, Message: "only png/jpeg/webp/gif images are supported"}
	}

	limited := io.LimitReader(resp.Body, int64(c.cfg.WebFetchMaxBodyBytes)+1)
	body, rErr := io.ReadAll(limited)
	if rErr != nil {
		return nil, "", &WebError{Code: CodeHTTPError, Message: "read body failed"}
	}
	if len(body) > c.cfg.WebFetchMaxBodyBytes {
		return nil, "", &WebError{Code: CodeResponseTooLarge, Message: "image exceeds size cap"}
	}
	return body, mediaType, nil
}

// imageContentType matches the header's media type (charset/params stripped) against
// the image allowlist, returning the canonical media type on a hit. svg is rejected
// (absent from the allowlist) so a script-bearing SVG never streams through.
func imageContentType(ct string) (string, bool) {
	media := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if _, ok := allowedImageContentTypes[media]; ok {
		return media, true
	}
	return "", false
}
