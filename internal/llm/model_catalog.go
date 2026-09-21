package llm

// model_catalog.go lists what a backend can serve, so the cockpit's model box can offer
// published ids instead of asking the operator to type one from memory. Ollama merges
// the signed-in bridge's locally available tags with its public cloud catalogue.
// OpenRouter and llama.cpp reuse the same GET /models round-trip the pricing and profile resolvers
// already make (fetchModels in pricing_source.go) — the difference is that those select
// ONE model during the decode pass, and this keeps the list.
//
// OpenRouter publishes context_length and pricing; llama.cpp publishes meta.n_ctx and no
// rates. Ollama's cloud tags publish neither, so profile discovery remains a separate
// per-model operation.

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
)

// ErrModelCatalogUnavailable marks a catalogue that could not be read. The cockpit
// degrades to a free-text model box — never to a fabricated list.
var ErrModelCatalogUnavailable = errors.New("model catalog unavailable")

const ollamaCloudTagsURL = "https://ollama.com/api/tags"

type ollamaTagsWire struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ModelCatalogEntry is one published model. ContextWindow is 0 when the provider does not
// publish it on the catalogue route, and HasPrice is false when it charges nothing per
// token (a local server) or publishes no parseable rate.
//
// TopProviderContextWindow is OpenRouter's top_provider.context_length, 0 when absent. It
// can be lower than ContextWindow -- qwen/qwen3-embedding-8b publishes 32768 and 32000
// (measured 2026-09-14) -- and an embedding input must fit the provider that serves it.
type ModelCatalogEntry struct {
	ID                       string
	ContextWindow            int
	TopProviderContextWindow int
	Price                    Price
	HasPrice                 bool
	SupportedVoices          []string
}

// FetchModelCatalog returns the provider's selectable models, sorted by id. Ollama's
// list combines baseURL's available tags with the public cloud tags. The API key is sent
// only for OpenRouter, and even there the route needs no credential (see
// FetchModelPrice) — it is passed because the endpoint accepts it.
func FetchModelCatalog(
	ctx context.Context, client *http.Client, provider, baseURL, apiKey string,
) ([]ModelCatalogEntry, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "ollama" {
		return fetchOllamaCatalog(ctx, client, baseURL)
	}
	catalogueKey := ""
	if provider == "openrouter" {
		catalogueKey = apiKey
	}
	wire, err := fetchModels(ctx, client, baseURL, catalogueKey, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModelCatalogUnavailable, err)
	}
	entries := make([]ModelCatalogEntry, 0, len(wire.Data))
	for _, m := range wire.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		entry := ModelCatalogEntry{
			ID: id, ContextWindow: m.ContextLength,
			TopProviderContextWindow: max(m.TopProvider.ContextLength, 0),
		}
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

func fetchOllamaCatalog(
	ctx context.Context, client *http.Client, baseURL string,
) ([]ModelCatalogEntry, error) {
	localTagsURL, err := ollamaAPIURL(baseURL, "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModelCatalogUnavailable, err)
	}
	local, err := fetchOllamaTags(ctx, client, localTagsURL)
	if err != nil {
		return nil, fmt.Errorf("%w: local tags: %w", ErrModelCatalogUnavailable, err)
	}
	cloud, err := fetchOllamaTags(ctx, client, ollamaCloudTagsURL)
	if err != nil {
		return nil, fmt.Errorf("%w: cloud tags: %w", ErrModelCatalogUnavailable, err)
	}

	byID := make(map[string]ModelCatalogEntry, len(local.Models)+len(cloud.Models))
	for _, model := range local.Models {
		if name := strings.TrimSpace(model.Name); name != "" {
			byID[name] = ModelCatalogEntry{ID: name}
		}
	}
	for _, model := range cloud.Models {
		name := ollamaCloudAlias(model.Name)
		if name != "" {
			byID[name] = ModelCatalogEntry{ID: name}
		}
	}
	entries := make([]ModelCatalogEntry, 0, len(byID))
	for _, entry := range byID {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

func fetchOllamaTags(ctx context.Context, client *http.Client, endpoint string) (ollamaTagsWire, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ollamaTagsWire{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ollamaTagsWire{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response
	if resp.StatusCode != http.StatusOK {
		return ollamaTagsWire{}, fmt.Errorf("GET /api/tags returned %d", resp.StatusCode)
	}
	var wire ollamaTagsWire
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxModelsResponseBytes)).Decode(&wire); err != nil {
		return ollamaTagsWire{}, fmt.Errorf("decode /api/tags: %w", err)
	}
	return wire, nil
}

func ollamaCloudAlias(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, ":") {
		return name + "-cloud"
	}
	return name + ":cloud"
}

// FetchOutputModalityCatalog lists the models OpenRouter publishes for one output modality:
// "transcription" (speech-to-text) or "speech" (text-to-speech). Those ids are absent from the
// default /models list, which is text-only; measured 2026-09-19, the filter returns 21 and 18
// models. Their rows carry a prompt rate with no unit in the payload, so no price is read: the
// picker shows the id rather than a number whose unit it would have to guess.
func FetchOutputModalityCatalog(
	ctx context.Context, client *http.Client, baseURL, modality string,
) ([]ModelCatalogEntry, error) {
	// No credential: the list is public, and a query we build is not a reason to send one.
	wire, err := fetchModels(ctx, client, baseURL, "", url.Values{"output_modalities": {modality}})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModelCatalogUnavailable, err)
	}
	entries := make([]ModelCatalogEntry, 0, len(wire.Data))
	for _, m := range wire.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			entry := ModelCatalogEntry{ID: id}
			seen := make(map[string]struct{}, len(m.SupportedVoices))
			for _, rawVoice := range m.SupportedVoices {
				voice := strings.TrimSpace(rawVoice)
				if voice == "" {
					continue
				}
				if _, duplicate := seen[voice]; duplicate {
					continue
				}
				seen[voice] = struct{}{}
				entry.SupportedVoices = append(entry.SupportedVoices, voice)
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}
