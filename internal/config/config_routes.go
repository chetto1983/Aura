package config

import (
	"net"
	"net/url"
	"strings"
)

// config_routes.go holds the model-backend route resolvers — the ONE-knob
// local↔cloud swap (D-28) shared by rerank and embeddings. Split out of config.go
// to keep that file under the 600-LOC cap (CLAUDE.md §No god class).

// RerankRoute resolves the rerank endpoint as a ONE-knob local↔cloud swap (D-28,
// mirroring the vision route): with AURA_RERANK_MODEL unset it is the local
// aura-rerank sidecar at RerankBaseURL with no auth; set AURA_RERANK_MODEL to a
// cloud model (e.g. cohere/rerank-4-fast) and it routes to the shared OpenRouter
// endpoint authenticated with the SINGLE OPENROUTER_API_KEY (LLM.APIKey) every
// cloud backend reuses. An explicitly-set non-loopback RerankBaseURL overrides the
// OpenRouter base for a custom cloud reranker.
func (c *Config) RerankRoute() (baseURL, apiKey, model string) {
	if strings.TrimSpace(c.RerankModel) == "" {
		return c.RerankBaseURL, "", "" // local sidecar, default model, no auth
	}
	base := c.RerankBaseURL
	if isLoopbackURL(base) { // model set but base still the local default → shared OpenRouter
		base = sharedCloudBase(c.LLM.BaseURL)
	}
	return base, c.LLM.APIKey, c.RerankModel
}

// EmbedRoute resolves the embeddings endpoint as a ONE-knob local↔cloud swap
// (D-28): with AURA_EMBED_MODEL unset it is the local embedding sidecar at
// Neo4j.EmbedURL with no auth; set AURA_EMBED_MODEL to a cloud model (e.g.
// a hosted embedding model) and it routes to the shared OpenRouter endpoint with
// the SINGLE OPENROUTER_API_KEY. A non-loopback EmbedURL overrides the OpenRouter
// base for a custom cloud embedder.
func (c *Config) EmbedRoute() (baseURL, apiKey, model string) {
	if strings.TrimSpace(c.Neo4j.EmbedModel) == "" {
		return c.Neo4j.EmbedURL, "", "" // local sidecar, no auth
	}
	base := c.Neo4j.EmbedURL
	if isLoopbackURL(base) {
		base = sharedCloudBase(c.LLM.BaseURL)
	}
	return base, c.LLM.APIKey, c.Neo4j.EmbedModel
}

// sharedCloudBase strips a trailing /v1 from the shared OpenRouter base. The
// rerank and embed clients append "/v1/<endpoint>" to their base (unlike the LLM
// and vision clients, which append the bare "/chat/completions" to a base that
// already carries /v1). Without this strip a cloud swap would yield a double
// "/v1/v1/<endpoint>" that 404s — and, for the fail-soft rerank client, silently
// degrades to identity order (the cloud reranker never actually runs).
func sharedCloudBase(llmBase string) string {
	return strings.TrimSuffix(strings.TrimRight(llmBase, "/"), "/v1")
}

// isLoopbackURL reports whether a base URL points at the local host (127.0.0.1/::1/
// localhost) — used by the route resolvers to decide when a set model should swap
// the local default base for the shared OpenRouter endpoint.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}
