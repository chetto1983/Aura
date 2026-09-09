package web

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// contentGate decides whether a response's Content-Type header may stream back, and
// returns the canonical media type on a hit. Each byte-proxy passes its own gate —
// images allow png/jpeg/webp/gif, data allows json/csv/text — so the allowlist stays
// next to the caller that justifies it rather than becoming one permissive union.
type contentGate func(header string) (string, bool)

// fetchBytes is the shared body of every byte-returning proxy (FetchImage, FetchData).
// It exists so the SSRF sequence is written ONCE: parse + scheme allowlist (D-15) →
// the hardened transport's dialContext (hostname blocklist → DNS resolve →
// classify-every → pin → dial only the pinned IP, transport.go) → reject any 3xx
// (CheckRedirect never auto-follows, so a redirect is surfaced here and refused,
// never silently chased to a private target) → the caller's content-type gate →
// size cap via io.LimitReader. Every failure is a sanitized *WebError carrying NO
// IP/host/redirect detail (D-26/27/28).
//
// kind names the resource in the two caller-facing error messages; unsupported is the
// message for a content-type the gate rejected.
func (c *Client) fetchBytes(ctx context.Context, convID, rawURL, kind, unsupported string, gate contentGate) ([]byte, string, error) {
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

	if isRedirect(resp.StatusCode) {
		return nil, "", &WebError{Code: CodeHTTPError, Reason: "redirect_not_followed", Message: kind + " redirect not followed"}
	}
	if resp.StatusCode/100 != 2 {
		return nil, "", &WebError{Code: CodeHTTPError, Message: "non-success status", StatusCode: resp.StatusCode}
	}

	mediaType, ok := gate(resp.Header.Get("Content-Type"))
	if !ok {
		return nil, "", &WebError{Code: CodeUnsupportedContent, Message: unsupported}
	}

	limited := io.LimitReader(resp.Body, int64(c.cfg.WebFetchMaxBodyBytes)+1)
	body, rErr := io.ReadAll(limited)
	if rErr != nil {
		return nil, "", &WebError{Code: CodeHTTPError, Message: "read body failed"}
	}
	if len(body) > c.cfg.WebFetchMaxBodyBytes {
		return nil, "", &WebError{Code: CodeResponseTooLarge, Message: kind + " exceeds size cap"}
	}
	return body, mediaType, nil
}

// mediaTypeIn matches a Content-Type header's media type (charset and other params
// stripped) against an allowlist, returning the canonical media type on a hit.
func mediaTypeIn(allowed map[string]struct{}) contentGate {
	return func(header string) (string, bool) {
		media := strings.ToLower(strings.TrimSpace(strings.SplitN(header, ";", 2)[0]))
		if _, ok := allowed[media]; ok {
			return media, true
		}
		return "", false
	}
}
