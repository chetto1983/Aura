package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrPricingUnavailable marks a rate that could not be read from the provider. The
// caller must degrade to the honest "n/a" (D-23) — never to a fabricated $0, and never
// to a stale hardcoded guess, which is what amendment #93 removed. It is worth a log
// line: something that should have been readable was not.
var ErrPricingUnavailable = errors.New("model pricing unavailable")

// ErrPricingNotApplicable marks a backend that publishes no rates because it charges
// nothing per token — a local llama.cpp/vLLM server. Distinct from Unavailable so a
// local boot does not warn about a catalogue it was never meant to have.
var ErrPricingNotApplicable = errors.New("model pricing not applicable to this backend")

// modelsWire is the GET /models projection. Only consumed fields are declared. The
// rates arrive as JSON STRINGS ("0.00000014"), not numbers, and input_cache_read is
// ABSENT for 155 of the 367 published models — both verified against the live payload
// on 2026-07-29, so neither is a defensive guess.
type modelsWire struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt         string `json:"prompt"`
			Completion     string `json:"completion"`
			InputCacheRead string `json:"input_cache_read"`
		} `json:"pricing"`
	} `json:"data"`
}

// BaseModelID strips the routing-variant suffix. `deepseek/deepseek-v4-flash:nitro` is
// what an operator configures and what the request sends, but NONE of the 367 published
// ids carry a `:` variant — the catalogue only ever lists the base model, so a lookup
// that skips this step always misses. Vendor and name never contain a colon.
func BaseModelID(model string) string {
	base, _, _ := strings.Cut(model, ":")
	return base
}

// FetchModelPrice reads the published rate for ONE model — the configured one. Aura
// uses a single model per process, so pulling and caching the whole 367-entry, ~600 KB
// catalogue would be waste; this selects during the decode pass.
//
// GET /models needs NO credential (verified: HTTP 200 with no Authorization header and
// again with a garbage bearer), so an empty apiKey is not an error here. The key is sent
// when present only because the endpoint accepts it.
func FetchModelPrice(ctx context.Context, client *http.Client, baseURL, apiKey, model string) (Price, error) {
	want := BaseModelID(model)
	if want == "" {
		return Price{}, fmt.Errorf("%w: empty model id", ErrPricingUnavailable)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return Price{}, fmt.Errorf("%w: %w", ErrPricingUnavailable, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Price{}, fmt.Errorf("%w: %w", ErrPricingUnavailable, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	if resp.StatusCode != http.StatusOK {
		return Price{}, fmt.Errorf("%w: GET /models returned %d", ErrPricingUnavailable, resp.StatusCode)
	}

	var wire modelsWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return Price{}, fmt.Errorf("%w: decode /models: %w", ErrPricingUnavailable, err)
	}

	for _, m := range wire.Data {
		if m.ID != want {
			continue
		}
		in, err := ratePer1M(m.Pricing.Prompt)
		if err != nil {
			return Price{}, fmt.Errorf("%w: prompt rate for %q: %w", ErrPricingUnavailable, want, err)
		}
		out, err := ratePer1M(m.Pricing.Completion)
		if err != nil {
			return Price{}, fmt.Errorf("%w: completion rate for %q: %w", ErrPricingUnavailable, want, err)
		}
		// Absent for 155 models; a zero CacheReadPer1M means "bill cache reads at the
		// prompt rate", which is what CostUSDValue does.
		cache, _ := ratePer1M(m.Pricing.InputCacheRead)
		return Price{InputPer1M: in, OutputPer1M: out, CacheReadPer1M: cache}, nil
	}
	return Price{}, fmt.Errorf("%w: %q not among the %d published models", ErrPricingUnavailable, want, len(wire.Data))
}

// ratePer1M converts a per-token rate string to USD per 1,000,000 tokens.
func ratePer1M(s string) (float64, error) {
	if strings.TrimSpace(s) == "" {
		return 0, errors.New("empty rate")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return v * 1_000_000, nil
}

// ResolvePricing fills c.Prices for the configured model from the live catalogue. It is
// called explicitly by the composition root rather than from Load() so that constructing
// a Config never touches the network (tests, `aura config show`, offline boots).
//
// A local backend has no such catalogue and genuinely costs nothing per token, so it is
// skipped with ErrPricingUnavailable rather than reported as a failure to investigate.
func (c *Config) ResolvePricing(ctx context.Context) error {
	if ReasoningTarget(c.Provider, c.BaseURL) == ReasoningTargetLlamaCpp {
		return fmt.Errorf("%w: local backend publishes no rates", ErrPricingNotApplicable)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	p, err := FetchModelPrice(ctx, client, c.BaseURL, c.APIKey, c.Model)
	if err != nil {
		return err
	}
	if c.Prices == nil {
		c.Prices = map[string]Price{}
	}
	// Keyed on the CONFIGURED id, because that is the string every CostUSD call site
	// passes (cfg.Model) — not the base id the catalogue publishes.
	if _, pinned := c.Prices[c.Model]; !pinned {
		c.Prices[c.Model] = p
	}
	return nil
}
