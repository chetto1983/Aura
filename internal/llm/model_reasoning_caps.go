package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// normalizeModelID removes an OpenRouter routing suffix while preserving the exact base id.
func normalizeModelID(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.IndexByte(model, ':'); i >= 0 {
		model = model[:i]
	}
	return model
}

// maxModelsResponseBytes caps the GET /models response read (defensive). The real
// OpenRouter list is multi-MB across ~400 models, so 16 MiB accommodates growth
// while bounding memory against a hostile or runaway upstream (Threat
// T-37E-05-UPSTREAM / T-37E-05-AVAIL — the /models JSON becomes the allowed-effort
// set, so its size and content are both untrusted-ish input).
const maxModelsResponseBytes = 16 << 20

// ReasoningCapability is one model's advertised reasoning surface, parsed from a
// GET /models entry (D-13 auto-detection, replacing the hard-coded placebo D-12
// forbids). SupportedEfforts is ALREADY clamped to the internal vocabulary — every
// token passed through the strict allowlist, unknowns dropped — so a caller can
// trust it as the model's allowed set without re-validating.
type ReasoningCapability struct {
	SupportedEfforts []ReasoningEffort // subset of {max,xhigh,high,medium,low,none}, allowlist-clamped
	DefaultEffort    ReasoningEffort   // "" when absent/unknown
	DefaultEnabled   bool
	Mandatory        bool     // true => reasoning cannot be turned off ("off"/none invalid)
	SupportedParams  []string // reasoning / include_reasoning / reasoning_effort
	InputModalities  []string // subset of {text,image,audio,file,video}, allowlist-clamped
}

// openRouterModelsResponse is the wire DTO for GET {BaseURL}/models. The nesting
// (data[].reasoning.{supported_efforts,default_effort,default_enabled,mandatory} +
// top-level supported_parameters) is operator-verified live (37E-RESEARCH.md P2.2 A);
// the fixture testdata/openrouter_models.json pins the exact bytes for the parse tests.
type openRouterModelsResponse struct {
	Data []openRouterModelEntry `json:"data"`
}

type openRouterModelEntry struct {
	ID           string `json:"id"`
	Architecture *struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	Reasoning *struct {
		SupportedEfforts []string `json:"supported_efforts"`
		DefaultEffort    string   `json:"default_effort"`
		DefaultEnabled   bool     `json:"default_enabled"`
		Mandatory        bool     `json:"mandatory"`
	} `json:"reasoning"`
	SupportedParameters []string `json:"supported_parameters"`
}

// allowedReasoningEfforts is the STRICT allowlist that maps an advertised OpenRouter
// effort token to an internal ReasoningEffort. Any token outside this set (a hostile
// or buggy upstream, or a future vocab OpenRouter adds that Aura does not model yet)
// is DROPPED, so a garbage token can never enter the validator's allowed set
// (Threat T-37E-05-UPSTREAM). The six keys are OpenRouter's own reasoning vocabulary
// and coincide 1:1 with the internal ReasoningEffort string values.
var allowedReasoningEfforts = map[string]ReasoningEffort{
	"max":    ReasoningEffortMax,
	"xhigh":  ReasoningEffortXHigh,
	"high":   ReasoningEffortHigh,
	"medium": ReasoningEffortMedium,
	"low":    ReasoningEffortLow,
	"none":   ReasoningEffortNone,
}

var allowedInputModalities = map[string]string{
	"text": "text", "image": "image", "audio": "audio", "file": "file", "video": "video",
}

func clampInputModalities(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		normalized, ok := allowedInputModalities[strings.ToLower(strings.TrimSpace(token))]
		if !ok || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// clampEffort maps one advertised token through the allowlist; an unknown token
// yields "" (dropped by the caller).
func clampEffort(token string) ReasoningEffort {
	if e, ok := allowedReasoningEfforts[strings.ToLower(strings.TrimSpace(token))]; ok {
		return e
	}
	return ""
}

// clampEfforts maps advertised tokens to the internal vocabulary, dropping unknowns
// and preserving order. It returns nil (not an empty slice) when nothing survives so
// the seam's "empty => detected=false" check is unambiguous.
func clampEfforts(tokens []string) []ReasoningEffort {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]ReasoningEffort, 0, len(tokens))
	for _, t := range tokens {
		if e := clampEffort(t); e != "" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseReasoningCapability projects one /models entry onto a ReasoningCapability. A
// model with no reasoning object yields empty SupportedEfforts (a non-reasoning model,
// or one that does not advertise reasoning) — never a panic on an absent nested object.
func parseReasoningCapability(m openRouterModelEntry) ReasoningCapability {
	rc := ReasoningCapability{SupportedParams: m.SupportedParameters}
	if m.Architecture != nil {
		rc.InputModalities = clampInputModalities(m.Architecture.InputModalities)
	}
	if m.Reasoning == nil {
		return rc
	}
	rc.SupportedEfforts = clampEfforts(m.Reasoning.SupportedEfforts)
	rc.DefaultEffort = clampEffort(m.Reasoning.DefaultEffort)
	rc.DefaultEnabled = m.Reasoning.DefaultEnabled
	rc.Mandatory = m.Reasoning.Mandatory
	return rc
}

// ModelCapabilityClient fetches, defensively parses, and TTL-caches the OpenRouter
// GET /models list, exposing per-model reasoning capability keyed by normalizeModelID.
// The fetch is NEVER per-turn: warmed once at boot and refreshed only past the TTL, so
// the two-stage validator and the capability endpoint both read from memory (D-13
// no-per-turn-fetch requirement). The mutex both serializes a concurrent cold fetch
// (no thundering herd) and makes the cache -race clean.
type ModelCapabilityClient struct {
	cfg        Config
	httpClient *http.Client
	ttl        time.Duration
	now        func() time.Time // injectable clock for cache-expiry tests
	mu         sync.Mutex
	cache      map[string]ReasoningCapability
	fetchedAt  time.Time
	ok         bool
}

// NewModelCapabilityClient builds a client over cfg (reusing BaseURL + APIKey +
// Headers, the same trust boundary as /chat/completions) with a bounded HTTP client:
// a connect-timeout dialer, a whole-call Timeout (this is a simple GET, not a stream,
// so a total timeout is safe and DoS-defensive), and DisableKeepAlives so idle
// conns do not linger and trip goleak on short test subsets.
func NewModelCapabilityClient(cfg Config, ttl time.Duration) *ModelCapabilityClient {
	connect := time.Duration(cfg.ConnectTimeoutSec) * time.Second
	if connect <= 0 {
		connect = 10 * time.Second
	}
	total := time.Duration(cfg.TotalTimeoutSec) * time.Second
	if total <= 0 {
		total = 30 * time.Second
	}
	return &ModelCapabilityClient{
		cfg: cfg,
		ttl: ttl,
		now: time.Now,
		httpClient: &http.Client{
			Timeout: total,
			Transport: &http.Transport{
				DialContext:       (&net.Dialer{Timeout: connect}).DialContext,
				DisableKeepAlives: true,
			},
		},
		cache: map[string]ReasoningCapability{},
	}
}

// ReasoningCapabilityFor returns the active model's advertised capability, refreshing
// the /models cache on a cold or expired read. ok=false when the model is absent from
// the list; a non-nil error marks a fetch/parse failure (the caller shows the safe
// floor in both cases). NEVER serves a stale cache on a failed refresh — a failure
// returns the error so the seam degrades to detected=false (Threat T-37E-05-AVAIL).
func (c *ModelCapabilityClient) ReasoningCapabilityFor(ctx context.Context, model string) (ReasoningCapability, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureFreshLocked(ctx); err != nil {
		return ReasoningCapability{}, false, err
	}
	rc, ok := c.cache[normalizeModelID(model)]
	return rc, ok, nil
}

// ensureFreshLocked refetches when the cache is cold or past the TTL. The caller holds
// c.mu. On a successful fetch it swaps in the new map and stamps fetchedAt; on failure
// it returns the error and leaves any prior cache untouched (but does NOT report it as
// fresh — the next call retries).
func (c *ModelCapabilityClient) ensureFreshLocked(ctx context.Context) error {
	if c.ok && c.now().Sub(c.fetchedAt) < c.ttl {
		return nil
	}
	caps, err := c.fetch(ctx)
	if err != nil {
		return err
	}
	c.cache = caps
	c.fetchedAt = c.now()
	c.ok = true
	return nil
}

// fetch performs the GET /models round-trip and parses it defensively (Authorization
// + attribution headers mirror the streaming client; body-size-capped; json.Decoder).
func (c *ModelCapabilityClient) fetch(ctx context.Context) (map[string]ReasoningCapability, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v) // HTTP-Referer + X-Title attribution (D-20)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("llm: GET /models: unexpected status %d", resp.StatusCode)
	}
	var parsed openRouterModelsResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxModelsResponseBytes))
	if err := dec.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("llm: decode /models: %w", err)
	}
	caps := make(map[string]ReasoningCapability, len(parsed.Data))
	for i := range parsed.Data {
		entry := parsed.Data[i]
		caps[normalizeModelID(entry.ID)] = parseReasoningCapability(entry)
	}
	return caps, nil
}

// ReasoningCapabilitySource is the neutral seam both the capability endpoint (plan 06)
// and the two-stage validator read. It is selected at boot by
// NewReasoningCapabilitySource per llm.ReasoningTarget(cfg.Provider, cfg.BaseURL).
// detected=false means the caller MUST show the safe floor {auto,off} — a nil source
// (unwired) or a failed detection both degrade, never 503 (37D D-09). The `auto`
// symbol is prepended by the endpoint/UI layer; it is not a ReasoningEffort.
type ReasoningCapabilitySource interface {
	AllowedEfforts(ctx context.Context) (efforts []ReasoningEffort, deflt ReasoningEffort, detected bool)
}

// openRouterReasoningCaps adapts the TTL-cached ModelCapabilityClient onto the seam
// for the active OpenRouter model: it maps supported_efforts → []ReasoningEffort
// (already clamped by the client), honors mandatory (strips none/off), and surfaces
// default_effort. A fetch failure, an absent model, or an empty advertised set all
// yield detected=false.
type openRouterReasoningCaps struct {
	client *ModelCapabilityClient
	model  string
}

var _ ReasoningCapabilitySource = (*openRouterReasoningCaps)(nil)

func (s *openRouterReasoningCaps) AllowedEfforts(ctx context.Context) ([]ReasoningEffort, ReasoningEffort, bool) {
	rc, ok, err := s.client.ReasoningCapabilityFor(ctx, s.model)
	if err != nil || !ok || len(rc.SupportedEfforts) == 0 {
		return nil, "", false
	}
	efforts := rc.SupportedEfforts
	if rc.Mandatory {
		efforts = excludeNone(efforts) // reasoning cannot be disabled → off is not offered
	}
	return efforts, rc.DefaultEffort, true
}

// excludeNone returns a copy of efforts without ReasoningEffortNone. It never mutates
// the shared cached slice (a fresh backing array), so the cache stays intact for the
// non-mandatory read path.
func excludeNone(efforts []ReasoningEffort) []ReasoningEffort {
	out := make([]ReasoningEffort, 0, len(efforts))
	for _, e := range efforts {
		if e != ReasoningEffortNone {
			out = append(out, e)
		}
	}
	return out
}
