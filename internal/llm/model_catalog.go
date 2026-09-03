package llm

// model_catalog.go lists what a backend actually serves, so the cockpit's model box can
// offer the ids the endpoint publishes instead of asking the operator to type one from
// memory. It reuses the same GET /models round-trip the pricing and profile resolvers
// already make (fetchModels in pricing_source.go) — the difference is that those select
// ONE model during the decode pass, and this keeps the list.
//
// All three supported providers answer the same OpenAI-compatible route: OpenRouter
// publishes context_length and pricing, llama.cpp publishes meta.n_ctx and no rates, and
// Ollama's /v1/models lists the pulled models with neither (its context window lives
// behind a per-model POST /api/show, which is one call per row and not worth it for a
// picker).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ErrModelCatalogUnavailable marks a catalogue that could not be read. The cockpit
// degrades to a free-text model box — never to a fabricated list.
var ErrModelCatalogUnavailable = errors.New("model catalog unavailable")

// ModelCatalogEntry is one published model. ContextWindow is 0 when the provider does not
// publish it on the catalogue route, and HasPrice is false when it charges nothing per
// token (a local server) or publishes no parseable rate.
type ModelCatalogEntry struct {
	ID            string
	ContextWindow int
	Price         Price
	HasPrice      bool
}

// FetchModelCatalog returns the models baseURL publishes, sorted by id. The API key is
// sent only for OpenRouter, and even there the route needs no credential (see
// FetchModelPrice) — it is passed because the endpoint accepts it.
func FetchModelCatalog(
	ctx context.Context, client *http.Client, provider, baseURL, apiKey string,
) ([]ModelCatalogEntry, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	catalogueKey := ""
	if provider == "openrouter" {
		catalogueKey = apiKey
	}
	wire, err := fetchModels(ctx, client, baseURL, catalogueKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModelCatalogUnavailable, err)
	}
	entries := make([]ModelCatalogEntry, 0, len(wire.Data))
	for _, m := range wire.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		entry := ModelCatalogEntry{ID: id, ContextWindow: m.ContextLength}
		if entry.ContextWindow <= 0 {
			entry.ContextWindow = m.Meta.ContextWindow
		}
		if entry.ContextWindow < 0 {
			entry.ContextWindow = 0
		}
		if provider == "openrouter" {
			in, inErr := ratePer1M(m.Pricing.Prompt)
			out, outErr := ratePer1M(m.Pricing.Completion)
			if inErr == nil && outErr == nil {
				cache, _ := ratePer1M(m.Pricing.InputCacheRead)
				entry.Price = Price{InputPer1M: in, OutputPer1M: out, CacheReadPer1M: cache}
				entry.HasPrice = true
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}
