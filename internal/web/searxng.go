package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SearchParams is the validated, model-supplied search request (D-09). Domains are
// hostnames only (D-12); Category is an enum (general|news, D-11); TimeRange is
// day|month|year. IncludeMetadata toggles the second result tier (D-08).
type SearchParams struct {
	Query           string
	MaxResults      int
	Category        string
	Language        string
	TimeRange       string
	Domains         []string
	IncludeMetadata bool
}

// Result is the model-visible search hit. Metadata is nil unless IncludeMetadata
// was set, so the default wire shape stays {title,url,snippet} (D-07/D-08).
type Result struct {
	Title    string          `json:"title"`
	URL      string          `json:"url"`
	Snippet  string          `json:"snippet"`
	Metadata *ResultMetadata `json:"metadata,omitempty"`
}

// ResultMetadata is the NORMALIZED second tier (D-08/D-10). PublishedAt is a
// pointer so a null/absent publishedDate omits the key rather than emitting "".
type ResultMetadata struct {
	Engine      string  `json:"engine,omitempty"`
	Score       float64 `json:"score,omitempty"`
	Category    string  `json:"category,omitempty"`
	PublishedAt *string `json:"published_at,omitempty"`
	Thumbnail   string  `json:"thumbnail,omitempty"`
}

// searxResult mirrors SearXNG's own JSON result schema. publishedDate is nullable
// (RESEARCH §Code Examples) so it is a *string.
type searxResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Engine        string  `json:"engine"`
	Score         float64 `json:"score"`
	Category      string  `json:"category"`
	PublishedDate *string `json:"publishedDate"`
	Thumbnail     string  `json:"thumbnail"`
}

type searxResponse struct {
	Results []searxResult `json:"results"`
}

// auraCategoryToSearXNG is the enum→raw map (D-10/D-11). Only general and news are
// accepted in Phase 7 — images is deferred (OQ3/D-11). An empty category defaults
// to general. A model-supplied engines/safesearch never appears here: those raw
// SearXNG params are NEVER passed through (T-07-23).
var auraCategoryToSearXNG = map[string]string{
	"":        "general",
	"general": "general",
	"news":    "news",
}

// validTimeRanges is the accepted time_range enum (day|month|year). An unknown
// value is dropped rather than forwarded so a malformed param cannot reach SearXNG.
var validTimeRanges = map[string]struct{}{"day": {}, "month": {}, "year": {}}

// errSearxUnreachable is the internal sentinel an unreachable/non-2xx SearXNG wraps;
// Search maps it to web_search_unavailable{searxng_unreachable} (D-06).
var errSearxUnreachable = errors.New("searxng unreachable")

// Search builds a SearXNG /search JSON query, parses the response, and post-filters
// by the requested domain set. It runs under the configured search deadline with
// at most one retry on a transient/408/429/5xx failure (D-14/D-42). Missing
// SEARXNG_URL → web_search_unavailable{searxng_not_configured}; an unreachable or
// non-2xx backend → searxng_unreachable. The backend URL/body is never leaked.
func (c *Client) Search(ctx context.Context, params SearchParams) ([]Result, error) {
	if strings.TrimSpace(c.cfg.SearxngURL) == "" {
		return nil, &WebError{Code: CodeSearchUnavailable, Reason: ReasonSearxngNotConfigured, Message: "search backend not configured"}
	}
	cat, ok := auraCategoryToSearXNG[params.Category]
	if !ok {
		return nil, &WebError{Code: CodeSearchUnavailable, Reason: "unsupported_category", Message: "category must be general or news"}
	}

	values := c.buildQuery(params, cat)
	// IncludeMetadata changes the cached shape, so it is part of the key — otherwise
	// a prior metadata-less call would serve a tier-1 result to a tier-2 request.
	key := cacheKey("search", searchCacheDiscriminator(values, params))
	if cached, hit := c.cache.get(key); hit {
		var res []Result
		if json.Unmarshal(cached, &res) == nil {
			return res, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.WebSearchTimeoutSec)*time.Second)
	defer cancel()

	resp, err := c.searxGet(ctx, values)
	if err != nil {
		return nil, &WebError{Code: CodeSearchUnavailable, Reason: ReasonSearxngUnreachable, Message: "search backend unavailable"}
	}

	results := c.mapResults(resp, params)
	if b, mErr := json.Marshal(results); mErr == nil {
		c.cache.set(key, b, searchCacheTTL)
	}
	return results, nil
}

// searchCacheTTL is the short fixed TTL for cached search responses (D-33).
const searchCacheTTL = 2 * time.Minute

// searchCacheDiscriminator folds the encoded query plus the result-shaping params
// (IncludeMetadata, MaxResults) into the raw cache key so two calls that hit the
// same backend query but want different result shapes do not alias.
func searchCacheDiscriminator(values url.Values, params SearchParams) string {
	return fmt.Sprintf("%s|meta=%t|max=%d", values.Encode(), params.IncludeMetadata, params.MaxResults)
}

// buildQuery assembles the url.Values: q (+ site: rewrite joined by OR for valid
// hostnames, D-12), format=json, the enum category, optional language/time_range,
// and pageno=1.
func (c *Client) buildQuery(params SearchParams, cat string) url.Values {
	q := params.Query
	if sites := siteRewrite(params.Domains); sites != "" {
		q = q + " (" + sites + ")"
	}
	v := url.Values{}
	v.Set("q", q)
	v.Set("format", "json")
	v.Set("categories", cat)
	if params.Language != "" {
		v.Set("language", params.Language)
	}
	if _, ok := validTimeRanges[params.TimeRange]; ok {
		v.Set("time_range", params.TimeRange)
	}
	v.Set("pageno", "1")
	return v
}

// siteRewrite turns the hostname-only domain set into a `site:a OR site:b` clause,
// rejecting any entry that carries a scheme or path (D-12). Returns "" when no
// valid domain survives.
func siteRewrite(domains []string) string {
	var sites []string
	for _, d := range domains {
		if h := validHostname(d); h != "" {
			sites = append(sites, "site:"+h)
		}
	}
	return strings.Join(sites, " OR ")
}

// validHostname returns the lower-cased hostname when d is a bare hostname (no
// scheme, no path, no port), else "" (D-12 — reject scheme/path).
func validHostname(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	if d == "" || strings.ContainsAny(d, "/:? ") {
		return ""
	}
	return d
}

// searxGet performs the GET with the Aura UA and one retry on a transient failure
// or a retryable status (408/429/5xx). A 4xx (non-408/429) is a hard, non-retried
// failure (D-42). It returns the decoded response or errSearxUnreachable.
func (c *Client) searxGet(ctx context.Context, values url.Values) (*searxResponse, error) {
	endpoint := c.cfg.SearxngURL + "?" + values.Encode()
	httpClient := &http.Client{} // deadline rides ctx, not the client (docker.go idiom)

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.userAgent())
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil || attempt == 1 {
				return nil, errSearxUnreachable
			}
			continue // transport failure → one retry within the deadline
		}
		out, decErr, retry := decodeSearx(resp)
		if retry {
			if ctx.Err() != nil || attempt == 1 {
				return nil, errSearxUnreachable
			}
			continue
		}
		if decErr != nil {
			return nil, errSearxUnreachable
		}
		return out, nil
	}
	return nil, errSearxUnreachable
}

// decodeSearx closes resp.Body and returns the decoded response. The retry bool is
// true for a retryable status (408/429/5xx); a non-retryable non-2xx returns a
// hard error with retry=false (D-42).
func decodeSearx(resp *http.Response) (out *searxResponse, err error, retry bool) {
	defer func() { _ = resp.Body.Close() }()
	if isRetryableStatus(resp.StatusCode) {
		return nil, errSearxUnreachable, true
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("searxng status %d: %w", resp.StatusCode, errSearxUnreachable), false
	}
	var sr searxResponse
	if derr := json.NewDecoder(io.LimitReader(resp.Body, maxSearxBodyBytes)).Decode(&sr); derr != nil {
		return nil, fmt.Errorf("decode searxng: %w", errSearxUnreachable), false
	}
	return &sr, nil, false
}

// maxSearxBodyBytes bounds the SearXNG response body read so a pathological backend
// cannot exhaust memory (defense-in-depth; the backend is in-network but untrusted
// on the wire, D-10).
const maxSearxBodyBytes = 4 << 20

// isRetryableStatus reports whether a status warrants the single retry: 408, 429,
// or any 5xx (D-42). A 4xx other than 408/429 is never retried.
func isRetryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code/100 == 5
}

// mapResults normalizes searxResult → Result, attaches metadata only when
// requested, and post-filters by the domain set (suffix match for subdomains,
// D-13 — fewer results rather than off-domain leak). MaxResults clamps the final
// slice when set.
func (c *Client) mapResults(resp *searxResponse, params SearchParams) []Result {
	out := make([]Result, 0, len(resp.Results))
	for i := range resp.Results {
		sr := &resp.Results[i]
		if !domainAllowed(sr.URL, params.Domains) {
			continue
		}
		r := Result{Title: sr.Title, URL: sr.URL, Snippet: sr.Content}
		if params.IncludeMetadata {
			r.Metadata = &ResultMetadata{
				Engine:      sr.Engine,
				Score:       sr.Score,
				Category:    sr.Category,
				PublishedAt: sr.PublishedDate,
				Thumbnail:   sr.Thumbnail,
			}
		}
		out = append(out, r)
		if params.MaxResults > 0 && len(out) >= params.MaxResults {
			break
		}
	}
	return out
}

// domainAllowed reports whether rawURL's host matches the domain set by exact or
// subdomain suffix match (D-13). An empty set allows everything. An unparseable
// URL is dropped (it cannot be proven on-domain).
func domainAllowed(rawURL string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}
