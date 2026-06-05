package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// DefaultCatalogURL is the skills.sh base for the /api/search JSON endpoint
// (D-11). The endpoint is the CLI's internal one (vercel-labs/skills#426 — no
// public API yet); the transport is isolated behind Search so the future public
// API is a drop-in swap.
const DefaultCatalogURL = "https://www.skills.sh"

// DefaultCatalogTimeoutSec bounds a catalog call when the config supplies 0.
// skills.sh cold latency was ~1.5s in spike 003; 15s is generous headroom while
// still failing-soft on a hung endpoint (T-11-03-D1).
const DefaultCatalogTimeoutSec = 15

// Catalog sentinel errors so callers classify without string-matching.
var (
	// ErrCatalogDisabled is returned by Search when the catalog is disabled via
	// the D-12 escape hatch (AURA_SKILL_CATALOG_DISABLE / disable-catalog).
	ErrCatalogDisabled = errors.New("skill catalog is disabled")
	// ErrEmptyQuery is returned for an empty/whitespace query, guarded before the
	// HTTP call (skills.sh 400s on empty — spike 003 Pitfall 5).
	ErrEmptyQuery = errors.New("catalog query must not be empty")
)

// CatalogItem is a single skills.sh search result. Only these fields are
// load-bearing (spike 003): the decoder is LAX, so drift fields (isDuplicate,
// count) are ignored rather than rejected.
type CatalogItem struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
	Source   string `json:"source"`
}

// CatalogConfig configures a CatalogClient. BaseURL defaults to DefaultCatalogURL;
// TimeoutSec to DefaultCatalogTimeoutSec; Disabled honors the D-12 escape hatch.
type CatalogConfig struct {
	BaseURL    string
	TimeoutSec int
	Disabled   bool
}

// CatalogClient is the skills.sh /api/search transport. It mirrors the
// sandboxagent.Client shape: a base URL + an *http.Client with a timeout. The
// HTTP shape never leaks to callers — they see only Search returning ranked
// CatalogItems — so swapping to the future public API (vercel-labs/skills#426)
// is contained here.
type CatalogClient struct {
	baseURL  string
	http     *http.Client
	disabled bool
}

// NewCatalogClient builds a CatalogClient from config. An injectable httpClient
// (nil → a default with the configured timeout) keeps the unit tests pointed at
// an httptest.Server.
func NewCatalogClient(cfg CatalogConfig, httpClient *http.Client) *CatalogClient {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultCatalogURL
	}
	if httpClient == nil {
		timeout := cfg.TimeoutSec
		if timeout <= 0 {
			timeout = DefaultCatalogTimeoutSec
		}
		httpClient = &http.Client{Timeout: time.Duration(timeout) * time.Second}
	}
	return &CatalogClient{baseURL: base, http: httpClient, disabled: cfg.Disabled}
}

// Disabled reports whether the catalog is turned off via the D-12 escape hatch
// (AURA_SKILL_CATALOG_DISABLE / disable-catalog). The action=catalog handler reads it
// to return enable-guidance without dialing.
func (c *CatalogClient) Disabled() bool { return c.disabled }

// Search queries skills.sh /api/search and returns results ranked by Installs
// descending. It guards an empty query BEFORE the call (the server 400s on
// empty, Pitfall 5), surfaces a non-2xx as an error with a LimitReader'd body
// snippet (T-11-03-I1/D1), and LAX-decodes the response with the default
// json decoder (unknown fields tolerated — schema drift in/out, Pitfall 5).
// When the catalog is disabled it returns ErrCatalogDisabled without dialing (D-12).
func (c *CatalogClient) Search(ctx context.Context, query string) ([]CatalogItem, error) {
	if c.disabled {
		return nil, ErrCatalogDisabled
	}
	if strings.TrimSpace(query) == "" {
		return nil, ErrEmptyQuery
	}

	endpoint := c.baseURL + "/api/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("catalog search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	// Lax decode: an anonymous wrapper over the load-bearing fields, using the
	// default json decoder (NOT strict/unknown-field-rejecting). Unknown
	// top-level keys (count) and per-item keys (isDuplicate) are ignored, never
	// rejected — the skills.sh schema drifts (Pitfall 5).
	var payload struct {
		Skills []CatalogItem `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("catalog search decode: %w", err)
	}

	// Rank by installs descending; SliceStable keeps the server's tie order.
	sort.SliceStable(payload.Skills, func(i, j int) bool {
		return payload.Skills[i].Installs > payload.Skills[j].Installs
	})
	return payload.Skills, nil
}
