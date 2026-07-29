package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrSpendUnavailable marks spend that could not be read from the provider. Callers
// render an honest absence — the same rule the per-turn cost follows: never a fabricated
// zero, because "you spent nothing" and "I could not tell" are different statements.
var ErrSpendUnavailable = errors.New("provider spend unavailable")

// ErrSpendNotApplicable marks a backend that bills nothing, so there is no spend to
// fetch. A local llama.cpp/vLLM server has no account behind it.
var ErrSpendNotApplicable = errors.New("provider spend not applicable to this backend")

// Spend is what the provider itself says this API key has cost. It is measured, not
// derived: the alternative is multiplying stored token counts by a rate table, which
// misses every request that did not go through the turn-persistence path — vision,
// rerank, embeddings, the completion critic — and silently under-reports.
type Spend struct {
	Total   float64 // this key, lifetime
	Daily   float64 // this key, current UTC day
	Weekly  float64
	Monthly float64
}

// keyWire is the GET /key projection. Verified live 2026-07-29: the endpoint answers to
// the ordinary INFERENCE key (no management key), which is why cumulative spend is
// reachable without provisioning a second credential.
type keyWire struct {
	Data struct {
		Usage        float64 `json:"usage"`
		UsageDaily   float64 `json:"usage_daily"`
		UsageWeekly  float64 `json:"usage_weekly"`
		UsageMonthly float64 `json:"usage_monthly"`
	} `json:"data"`
}

// FetchSpend reads what the provider has billed this key. Note the scope: THIS key, not
// the account. On a shared account that distinction is the whole point — measured on the
// live account, an unfiltered account total read $3.6553 while this key accounted for
// $0.7326, so an account-wide figure would over-report Aura's cost by 5x.
func FetchSpend(ctx context.Context, client *http.Client, baseURL, apiKey string) (Spend, error) {
	if strings.TrimSpace(apiKey) == "" {
		return Spend{}, fmt.Errorf("%w: no API key configured", ErrSpendUnavailable)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/key", nil)
	if err != nil {
		return Spend{}, fmt.Errorf("%w: %w", ErrSpendUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return Spend{}, fmt.Errorf("%w: %w", ErrSpendUnavailable, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	if resp.StatusCode != http.StatusOK {
		return Spend{}, fmt.Errorf("%w: GET /key returned %d", ErrSpendUnavailable, resp.StatusCode)
	}

	var wire keyWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return Spend{}, fmt.Errorf("%w: decode /key: %w", ErrSpendUnavailable, err)
	}
	return Spend{
		Total:   wire.Data.Usage,
		Daily:   wire.Data.UsageDaily,
		Weekly:  wire.Data.UsageWeekly,
		Monthly: wire.Data.UsageMonthly,
	}, nil
}

// Spend reads the provider-reported spend for the configured key, or reports why it
// cannot. It is a live call with no cache: the figure it answers is "what has this key
// cost", which changes with every turn, so a stale one would be worse than none.
func (c *Config) Spend(ctx context.Context) (Spend, error) {
	if ReasoningTarget(c.Provider, c.BaseURL) == ReasoningTargetLlamaCpp {
		return Spend{}, fmt.Errorf("%w: local backend bills nothing", ErrSpendNotApplicable)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return FetchSpend(ctx, client, c.BaseURL, c.APIKey)
}
