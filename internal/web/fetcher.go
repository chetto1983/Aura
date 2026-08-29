package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxRedirectHops bounds the manual redirect loop (D-29 — "cap hops"; PRD suggests
// 5). Each hop re-enters the SSRF gate, so the cap also bounds revalidation work.
const maxRedirectHops = 5

// Page is the model-visible web_fetch result (D-17/D-19/D-22). Warning is non-empty
// only for a soft downgrade (low_content) — it is never an error channel. Links are
// deduped normalized absolute URL strings (D-19), never {text,url} objects.
type Page struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	ContentMD string   `json:"content_md"`
	Links     []string `json:"links"`
	Warning   string   `json:"warning,omitempty"`
}

// allowedSchemes is the fetch scheme allowlist (D-15). Anything else is
// unsupported_scheme with no dial.
var allowedSchemes = map[string]struct{}{"http": {}, "https": {}}

// allowedContentTypes is the MIME allowlist gated BEFORE reading the body (D-16,
// Pitfall 6). The Content-Type header is matched on its media type only (params
// like charset are stripped).
var allowedContentTypes = map[string]struct{}{
	"text/html":             {},
	"application/xhtml+xml": {},
}

// gatedBody is a response that cleared the Content-Type and size gates, carried
// together with the lane it was classified into so the caller renders it as what the
// origin declared rather than re-sniffing the bytes.
type gatedBody struct {
	data  []byte
	media string
	kind  contentKind
}

// errRetryable is the internal sentinel a transient/408/429/5xx response wraps so
// the one-retry loop can distinguish a retryable failure from a hard one (D-42).
var errRetryable = errors.New("retryable fetch failure")

// Fetch runs the full security state machine: parse + scheme allowlist (D-15) →
// per-hop SSRF validate+pin via the hardened transport + manual redirect
// revalidation (D-29) → Content-Type allowlist (D-16) → size cap (D-16) →
// readability→markdown (D-17/D-19/D-22). It runs under the configured fetch
// deadline with at most one retry on a transient/408/429/5xx response and no retry
// on an SSRF/4xx/config error (D-23/D-42). A per-host concurrency cap bounds
// in-flight fetches to one origin (D-36).
func (c *Client) Fetch(ctx context.Context, convID, rawURL string) (Page, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return Page{}, &WebError{Code: CodeHTTPError, Reason: ReasonInvalidTarget, Message: "could not parse URL"}
	}
	if _, ok := allowedSchemes[strings.ToLower(u.Scheme)]; !ok {
		return Page{}, &WebError{Code: CodeUnsupportedScheme, Message: "only http and https are supported"}
	}

	if cached, ok := c.fetchFromCache(rawURL); ok {
		return cached, nil
	}

	release, ok := c.hosts.acquire(ctx, u.Hostname())
	if !ok {
		return Page{}, &WebError{Code: CodeTimeout, Message: "fetch cancelled"}
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.WebFetchTimeoutSec)*time.Second)
	defer cancel()

	gated, finalURL, err := c.fetchBody(ctx, convID, u)
	if err != nil {
		return Page{}, err
	}

	page, err := renderPage(gated, finalURL)
	if err != nil {
		return Page{}, err
	}
	c.fetchToCache(rawURL, page)
	return page, nil
}

// renderPage turns a gated body into a Page along the lane its Content-Type chose.
// HTML goes through readability→markdown; readable non-HTML is fenced verbatim
// (fetcher_text.go). A text body yields no Title and no Links: inventing either would
// mean parsing bytes this lane exists precisely NOT to parse.
func renderPage(gated gatedBody, finalURL *url.URL) (Page, error) {
	if gated.kind == kindText {
		md, warning := renderText(gated.data, gated.media)
		return Page{URL: finalURL.String(), ContentMD: md, Warning: warning}, nil
	}
	title, md, links, warning, err := ExtractMarkdown(gated.data, finalURL)
	if err != nil {
		return Page{}, err
	}
	return Page{Title: title, URL: finalURL.String(), ContentMD: md, Links: links, Warning: warning}, nil
}

// fetchBody drives the manual redirect loop and gates the response, returning the
// size-capped body and the final URL. It runs the one-retry policy around the
// whole hop sequence: a retryable failure triggers exactly one re-attempt within
// the deadline (D-42).
func (c *Client) fetchBody(ctx context.Context, convID string, start *url.URL) (gatedBody, *url.URL, error) {
	for attempt := range 2 {
		gated, finalURL, err := c.doHops(ctx, convID, start)
		if err == nil {
			return gated, finalURL, nil
		}
		if errors.Is(err, errRetryable) && attempt == 0 && ctx.Err() == nil {
			continue // one retry within the deadline
		}
		return gatedBody{}, nil, c.classifyFetchErr(err)
	}
	return gatedBody{}, nil, &WebError{Code: CodeHTTPError, Message: "fetch failed"}
}

// doHops issues GETs following each 3xx Location manually, re-entering the SSRF
// gate (validateAndPin) for every hop BEFORE dialing the next target (D-29). The
// first hop is dialed by the hardened transport (whose dialContext pins it); a
// redirect to a blocked target is rejected AT THE HOP — the blocked target is
// never dialed. On a 2xx it gates Content-Type and size, then returns the body.
func (c *Client) doHops(ctx context.Context, convID string, current *url.URL) (gatedBody, *url.URL, error) {
	reqCtx := withConvID(ctx, convID)
	for range maxRedirectHops {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, current.String(), nil)
		if err != nil {
			return gatedBody{}, nil, err
		}
		req.Header.Set("User-Agent", c.userAgent())

		resp, err := c.transport.client.Do(req)
		if err != nil {
			return gatedBody{}, nil, classifyTransportErr(err)
		}

		if isRedirect(resp.StatusCode) {
			next, rErr := c.resolveRedirect(ctx, convID, current, resp)
			_ = resp.Body.Close()
			if rErr != nil {
				return gatedBody{}, nil, rErr
			}
			current = next
			continue
		}

		gated, gErr := gateAndRead(resp, c.cfg.WebFetchMaxBodyBytes)
		_ = resp.Body.Close()
		if gErr != nil {
			return gatedBody{}, nil, gErr
		}
		return gated, current, nil
	}
	return gatedBody{}, nil, &WebError{Code: CodeHTTPError, Reason: "too_many_redirects", Message: "redirect limit exceeded"}
}

// resolveRedirect reads the Location header, resolves it against the current URL,
// re-checks the scheme, and re-enters validateAndPin for the next host. A block
// returns blocked_url{redirect_to_blocked_target} (D-26/D-29) WITHOUT dialing the
// target. The returned URL is the validated next hop.
func (c *Client) resolveRedirect(ctx context.Context, convID string, current *url.URL, resp *http.Response) (*url.URL, error) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return nil, &WebError{Code: CodeHTTPError, Message: "redirect without Location"}
	}
	ref, err := url.Parse(loc)
	if err != nil {
		return nil, &WebError{Code: CodeBlockedURL, Reason: ReasonInvalidTarget, Message: "invalid redirect target"}
	}
	next := current.ResolveReference(ref)
	if _, ok := allowedSchemes[strings.ToLower(next.Scheme)]; !ok {
		return nil, &WebError{Code: CodeUnsupportedScheme, Message: "redirect to unsupported scheme"}
	}
	if _, reason := c.transport.guard.validateAndPin(ctx, convID, next.Hostname()); reason != "" {
		return nil, &WebError{Code: CodeBlockedURL, Reason: ReasonRedirectToBlocked, Message: "redirect blocked"}
	}
	return next, nil
}

// gateAndRead enforces the Content-Type allowlist BEFORE reading the body, then
// reads through an io.LimitReader(cap+1) so a body that fills the extra byte is
// rejected as response_too_large (D-16, Pitfall 6). A retryable status wraps
// errRetryable; a non-retryable non-2xx is a hard http_error.
func gateAndRead(resp *http.Response, capBytes int) (gatedBody, error) {
	if isRetryableStatus(resp.StatusCode) {
		return gatedBody{}, errRetryable
	}
	if resp.StatusCode/100 != 2 {
		return gatedBody{}, &WebError{Code: CodeHTTPError, Message: "non-success status", StatusCode: resp.StatusCode}
	}
	media, kind := classifyContentType(resp.Header.Get("Content-Type"))
	if kind == kindUnsupported {
		return gatedBody{}, &WebError{Code: CodeUnsupportedContent, Message: "only HTML and readable text content is supported"}
	}
	limited := io.LimitReader(resp.Body, int64(capBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return gatedBody{}, &WebError{Code: CodeHTTPError, Message: "read body failed"}
	}
	if len(body) > capBytes {
		return gatedBody{}, &WebError{Code: CodeResponseTooLarge, Message: "response exceeds size cap"}
	}
	return gatedBody{data: body, media: media, kind: kind}, nil
}

// isRedirect reports whether status is a 3xx that carries a Location to follow.
func isRedirect(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// classifyTransportErr maps a transport-layer error to either an SSRF block (when
// the hardened dialContext rejected the target — never retried) or a retryable
// network failure (one retry within the deadline, D-42).
func classifyTransportErr(err error) error {
	var ie *internalError
	if errors.As(err, &ie) {
		return ie // SSRF/blocked — non-retryable, sanitized by the caller
	}
	// A peer-side stream reset is DETERMINISTIC, not transient: the same request
	// from the same client earns the same RST_STREAM every time. Retrying it only
	// spends the deadline and then reports `timeout`, hiding the real cause — the
	// exact failure that made 6 production web_fetch calls look like network
	// trouble when the peer was in fact refusing the User-Agent in 0.15s.
	if peerResetStream(err) {
		return &WebError{
			Code:    CodeHTTPError,
			Reason:  ReasonPeerResetStream,
			Message: "the site closed the connection without answering",
		}
	}
	// Wrap (don't replace) errRetryable so the one-retry loop still matches it via
	// errors.Is, while the underlying deadline/net-timeout survives for classifyFetchErr
	// to surface as CodeTimeout instead of a generic http_error (the retry guard already
	// suppresses a pointless retry once ctx is done).
	return errors.Join(errRetryable, err)
}

// classifyFetchErr is the terminal mapper: a *WebError or *internalError passes
// through (the caller sanitizes), a leftover errRetryable becomes http_error, and
// a deadline becomes timeout.
func (c *Client) classifyFetchErr(err error) error {
	if we, ok := AsWebError(err); ok {
		return we
	}
	if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
		return &WebError{Code: CodeTimeout, Message: "fetch timed out"}
	}
	return &WebError{Code: CodeHTTPError, Message: "fetch failed"}
}

// peerResetStream reports whether err is the peer tearing down an established
// HTTP/2 stream. net/http vendors its own copy of x/net/http2, so http2.StreamError
// is not importable and the error text is the only signal available — matching on
// the protocol's own constant names, not on prose, so this is stable across Go
// releases in a way a natural-language match would not be.
func peerResetStream(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, marker := range []string{
		"INTERNAL_ERROR", // RST_STREAM code 2 — the WAF refusal shape
		"stream error",   // http2 stream-level failure envelope
		"PROTOCOL_ERROR", // RST_STREAM code 1
		"REFUSED_STREAM", // RST_STREAM code 7
		"http2: server sent GOAWAY",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// isNetTimeout reports whether err is a net.Error timeout.
func isNetTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// fetchFromCache returns a cached Page for rawURL when present and unexpired. The
// cache stores the marshalled Page so a hit reconstructs the full result without a
// second fetch (D-31).
func (c *Client) fetchFromCache(rawURL string) (Page, bool) {
	b, ok := c.cache.get(cacheKey("fetch", rawURL))
	if !ok {
		return Page{}, false
	}
	var p Page
	if json.Unmarshal(b, &p) != nil {
		return Page{}, false
	}
	return p, true
}

// fetchToCache stores the Page under a bounded default TTL (D-33). A marshal
// failure is silently skipped — the cache is an optimization, never a correctness
// dependency.
func (c *Client) fetchToCache(rawURL string, p Page) {
	if b, err := json.Marshal(p); err == nil {
		c.cache.set(cacheKey("fetch", rawURL), b, 0) // 0 → defaultCacheTTL
	}
}
