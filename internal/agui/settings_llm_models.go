package agui

// settings_llm_models.go answers "what can this endpoint actually serve" for the cockpit's
// model box (GET /api/settings/llm-models). All three supported providers publish an
// OpenAI-compatible catalogue, so one route covers OpenRouter, llama.cpp and Ollama; only
// OpenRouter publishes rates, and those ride along because choosing a model without seeing
// its price is how a routing decision becomes a bill nobody predicted.
//
// The probe target is the base URL the operator has in the FORM, not the one in
// aura.settings: the point is to check a route before saving it. That makes this GET an
// outbound request to an operator-supplied host, which is why it is mounted behind
// governance.write like the settings mutations rather than governance.read — the same
// principal who may point the daemon's chat traffic at a host may point this probe at it.
// The OpenRouter key never comes from the client; it is read server-side from
// aura.settings, then the environment.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// modelCatalogTimeout bounds the probe. OpenRouter's catalogue is ~600 KB over the wire
// and a cold local llama.cpp answers in milliseconds; a route that cannot answer in this
// window is a route worth reporting as unreachable.
const modelCatalogTimeout = 20 * time.Second

var errUnsupportedCatalogProvider = errors.New(
	"provider must be openrouter, llamacpp, or ollama",
)

// modelCatalogFetcher is the outbound seam (llm.FetchModelCatalog in production). Tests
// replace it so the handler's branches are exercised without a network.
type modelCatalogFetcher func(
	ctx context.Context, provider, baseURL, apiKey string,
) ([]llm.ModelCatalogEntry, error)

type modelCatalogEntryDTO struct {
	ID            string  `json:"id"`
	ContextWindow int     `json:"context_window,omitempty"`
	InputPer1M    float64 `json:"input_per_1m,omitempty"`
	OutputPer1M   float64 `json:"output_per_1m,omitempty"`
	HasPrice      bool    `json:"has_price"`
}

type modelCatalogDTO struct {
	Provider string                 `json:"provider"`
	BaseURL  string                 `json:"base_url"`
	Models   []modelCatalogEntryDTO `json:"models"`
}

func (s *Server) handleListLLMModels(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	baseURL := strings.TrimSpace(r.URL.Query().Get("base_url"))
	switch provider {
	case "openrouter", "llamacpp", "ollama":
	default:
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": errUnsupportedCatalogProvider.Error()})
		return
	}
	if err := validateCatalogBaseURL(baseURL); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	apiKey := ""
	if provider == "openrouter" {
		value, err := s.effectiveSettingValue(r.Context(), "OPENROUTER_API_KEY")
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "settings store unavailable"})
			return
		}
		apiKey = value
	}
	ctx, cancel := context.WithTimeout(r.Context(), modelCatalogTimeout)
	defer cancel()
	entries, err := s.fetchModelCatalog(ctx, provider, baseURL, apiKey)
	if err != nil {
		// The reason is the whole value of this response when it fails: "GET /models
		// returned 401" tells the operator to fix the key, "connection refused" tells them
		// the container is down. Both are about a host they typed themselves.
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := modelCatalogDTO{
		Provider: provider,
		BaseURL:  baseURL,
		Models:   make([]modelCatalogEntryDTO, 0, len(entries)),
	}
	for _, entry := range entries {
		out.Models = append(out.Models, modelCatalogEntryDTO{
			ID:            entry.ID,
			ContextWindow: entry.ContextWindow,
			InputPer1M:    entry.Price.InputPer1M,
			OutputPer1M:   entry.Price.OutputPer1M,
			HasPrice:      entry.HasPrice,
		})
	}
	writeJSON(w, out)
}

// fetchModelCatalog runs the wired seam, or the real HTTP catalogue when none is wired.
func (s *Server) fetchModelCatalog(
	ctx context.Context, provider, baseURL, apiKey string,
) ([]llm.ModelCatalogEntry, error) {
	if s.modelCatalog != nil {
		return s.modelCatalog(ctx, provider, baseURL, apiKey)
	}
	client := &http.Client{Timeout: modelCatalogTimeout}
	return llm.FetchModelCatalog(ctx, client, provider, baseURL, apiKey)
}

// validateCatalogBaseURL mirrors the primary-route check in cmd/aura/serve_settings.go: an
// absolute http(s) URL with a host, no credentials, no query and no fragment. A probe must
// not accept a shape the route itself would reject.
func validateCatalogBaseURL(baseURL string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("base URL must be an absolute http(s) URL")
	}
	return nil
}
