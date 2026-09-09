package web

import "context"

// allowedDataContentTypes is the MIME allowlist the data proxy gates BEFORE
// streaming a body back to the cockpit.
//
// text/html and image/svg+xml are deliberately ABSENT, and the reason is the same
// for both: /api/fetch answers from Aura's OWN origin, so any document it relayed
// would execute with Aura's origin and the operator's cookies. The proxy carries
// data — never markup a browser could be talked into running.
var allowedDataContentTypes = map[string]struct{}{
	"application/json":     {},
	"application/geo+json": {},
	"application/ld+json":  {},
	"application/xml":      {},
	"text/xml":             {},
	"text/csv":             {},
	"text/plain":           {},
}

// FetchData fetches an external data document (JSON/XML/CSV/plain text) through the
// SAME SSRF defense web_fetch and the image proxy use — never a fresh http.Get. It
// returns the raw bytes plus the matched media type; see fetchBytes for the full
// guard sequence and the sanitized error contract.
func (c *Client) FetchData(ctx context.Context, convID, rawURL string) ([]byte, string, error) {
	return c.fetchBytes(ctx, convID, rawURL, "data",
		"only json, xml, csv and plain text are supported",
		mediaTypeIn(allowedDataContentTypes))
}
