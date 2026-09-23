package config

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// config_routes.go holds the model-backend route resolvers — the ONE-knob
// local↔cloud swap (D-28). Split out of config.go to keep that file under the
// 600-LOC cap (CLAUDE.md §No god class).

// EmbedRoute resolves the embeddings endpoint as a ONE-knob local↔cloud swap (D-28),
// the same shape STT and TTS already use: the cloud MODEL is the switch, and the local
// base is never a candidate for the cloud route. Empty model means the local sidecar at
// Embed.BaseURL with no auth; a model set means the cloud route — Embed.CloudBaseURL if
// the operator named a non-OpenRouter embedder, otherwise OpenRouter itself — always
// with the single OPENROUTER_API_KEY.
//
// "Otherwise OpenRouter itself" used to be "otherwise the chat LLM's base", and the
// cockpit's OpenRouter option writes exactly that empty cloud base. Measured on the lab VM
// 2026-09-23, whose chat route is Ollama: choosing OpenRouter sent embeddings to Ollama.
func (c *Config) EmbedRoute() (baseURL, apiKey, model string) {
	return ResolveEmbedRoute(c.Embed, c.LLM.APIKey)
}

// ResolveEmbedRoute exposes the daemon's route contract to processes that read the same
// aura.settings rows without loading the daemon's full configuration.
func ResolveEmbedRoute(embed EmbedConfig, apiKey string) (baseURL, credential, model string) {
	model = strings.TrimSpace(embed.CloudModel)
	if model == "" {
		return embed.BaseURL, "", "" // local sidecar, no auth
	}
	base := strings.TrimSpace(embed.CloudBaseURL)
	if base == "" {
		base = llm.DefaultBaseURL
	}
	// The embed client appends "/v1/<endpoint>" to its base, unlike the LLM and vision
	// clients, which append "/chat/completions" to a base that already carries /v1.
	// Without this strip the request would go to "/v1/v1/embeddings" and 404.
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1"), apiKey, model
}
