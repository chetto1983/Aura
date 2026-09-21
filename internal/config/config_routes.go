package config

import "strings"

// config_routes.go holds the model-backend route resolvers — the ONE-knob
// local↔cloud swap (D-28). Split out of config.go to keep that file under the
// 600-LOC cap (CLAUDE.md §No god class).

// EmbedRoute resolves the embeddings endpoint as a ONE-knob local↔cloud swap (D-28),
// the same shape STT and TTS already use: the cloud MODEL is the switch, and the local
// base is never a candidate for the cloud route. Empty model means the local sidecar at
// Embed.BaseURL with no auth; a model set means the cloud route — Embed.CloudBaseURL if
// the operator named a non-OpenRouter embedder, otherwise the shared LLM route — always
// with the single OPENROUTER_API_KEY.
//
// The previous version chose the base by asking whether Embed.BaseURL looked like
// loopback. It does not on the shipped product (compose.yaml:119 hands the daemon a
// Compose DNS name), so a cloud model was sent to the local sidecar, which answers it
// with local vectors and no error. See EmbedConfig for the measurement.
func (c *Config) EmbedRoute() (baseURL, apiKey, model string) {
	return ResolveEmbedRoute(c.Embed, c.LLM.BaseURL, c.LLM.APIKey)
}

// ResolveEmbedRoute exposes the daemon's route contract to processes that read the same
// aura.settings rows without loading the daemon's full configuration.
func ResolveEmbedRoute(embed EmbedConfig, llmBaseURL, apiKey string) (baseURL, credential, model string) {
	model = strings.TrimSpace(embed.CloudModel)
	if model == "" {
		return embed.BaseURL, "", "" // local sidecar, no auth
	}
	base := strings.TrimSpace(embed.CloudBaseURL)
	if base == "" {
		base = sharedCloudBase(llmBaseURL)
	}
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1"), apiKey, model
}

// sharedCloudBase strips a trailing /v1 from the shared OpenRouter base. The
// embed client appends "/v1/<endpoint>" to its base (unlike the LLM and vision
// clients, which append the bare "/chat/completions" to a base that already
// carries /v1). Without this strip a cloud swap would yield a double
// "/v1/v1/<endpoint>" that 404s.
func sharedCloudBase(llmBase string) string {
	return strings.TrimSuffix(strings.TrimRight(llmBase, "/"), "/v1")
}
