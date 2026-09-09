package web

import "context"

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
// hole D-09 forbids. It returns the raw bytes plus the matched media type; see
// fetchBytes for the full guard sequence (scheme allowlist → DNS-pinned dial →
// redirect refusal → content gate → size cap) and the sanitized error contract.
func (c *Client) FetchImage(ctx context.Context, convID, rawURL string) ([]byte, string, error) {
	return c.fetchBytes(ctx, convID, rawURL, "image",
		"only png/jpeg/webp/gif images are supported",
		mediaTypeIn(allowedImageContentTypes))
}
