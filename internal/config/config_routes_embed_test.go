package config

import "testing"

// The defect this file exists for, measured on a live appliance 2026-09-21: setting only
// the cloud model sent that model name and the OpenRouter key to the LOCAL sidecar,
// because the old route picked its base by asking whether AURA_EMBED_BASE_URL looked like
// loopback -- and on the shipped product it does not. compose.yaml hands the daemon
// http://aura-llama-embed:8081, a Compose DNS name. llama.cpp then answered with local
// EmbeddingGemma vectors and no error whatsoever: three probes (local model, cloud model
// with an Authorization header, and an invented model name) returned the byte-identical
// vector. Green healthchecks, wrong vector space.
func TestEmbedRouteNeverSendsACloudModelToTheLocalSidecar(t *testing.T) {
	const composeSidecar = "http://aura-llama-embed:8081"
	cfg := &Config{}
	cfg.Embed.BaseURL = composeSidecar
	cfg.Embed.CloudModel = "qwen/qwen3-embedding-8b"
	cfg.LLM.BaseURL = "https://openrouter.ai/api/v1"
	cfg.LLM.APIKey = "sk-or-v1-test"

	base, key, model := cfg.EmbedRoute()
	if base == composeSidecar {
		t.Fatalf("a cloud model resolved to the local sidecar %q: the corpus would be embedded locally while the operator believes it is on %q", base, model)
	}
	if base != "https://openrouter.ai/api" {
		t.Errorf("cloud base = %q, want the shared route without /v1 (this client appends /v1/embeddings)", base)
	}
	if key != "sk-or-v1-test" || model != "qwen/qwen3-embedding-8b" {
		t.Errorf("cloud route = (%q, %q), want the shared credential and the requested model", key, model)
	}
}

func TestEmbedRouteStaysLocalWithoutACloudModel(t *testing.T) {
	cfg := &Config{}
	cfg.Embed.BaseURL = "http://aura-llama-embed:8081"
	cfg.LLM.BaseURL = "https://openrouter.ai/api/v1"
	cfg.LLM.APIKey = "sk-or-v1-test"

	base, key, model := cfg.EmbedRoute()
	// No credential may ride to the local sidecar, and no model name either: llama.cpp
	// ignores both, so sending them can only ever mislead a reader of the wire.
	if base != "http://aura-llama-embed:8081" || key != "" || model != "" {
		t.Errorf("local route = (%q, %q, %q), want the sidecar with no auth and no model", base, key, model)
	}
}

// A non-OpenRouter embedder is a real deployment, and it is the case the old loopback
// heuristic was trying to serve. It now has a field of its own instead of being inferred.
func TestEmbedRouteHonoursAnExplicitCloudEndpoint(t *testing.T) {
	cfg := &Config{}
	cfg.Embed.BaseURL = "http://aura-llama-embed:8081"
	cfg.Embed.CloudModel = "vendor/embed-1"
	cfg.Embed.CloudBaseURL = "https://embed.example.internal/v1"
	cfg.LLM.BaseURL = "https://openrouter.ai/api/v1"
	cfg.LLM.APIKey = "sk-or-v1-test"

	base, _, _ := cfg.EmbedRoute()
	// Trailing /v1 is stripped whoever supplied it: this client appends /v1/embeddings,
	// and the doubled path 404s -- the exact trap .env.example still documents wrongly.
	if base != "https://embed.example.internal" {
		t.Errorf("explicit cloud base = %q, want it without the trailing /v1", base)
	}
}

// Measured on the lab VM 2026-09-23: the chat route was Ollama
// (AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1), and the cockpit's OpenRouter
// option -- which writes an EMPTY cloud base -- resolved embeddings to that Ollama server
// under an OpenRouter model id. Changing the chat model would also have re-routed embeddings.
func TestEmbedRouteOpenRouterOptionIgnoresTheChatBase(t *testing.T) {
	cfg := &Config{}
	cfg.Embed.BaseURL = "http://aura-llama-embed:8081"
	cfg.Embed.CloudModel = "qwen/qwen3-embedding-8b"
	cfg.LLM.BaseURL = "http://host.docker.internal:11434/v1"
	cfg.LLM.APIKey = "sk-or-v1-test"

	base, key, model := cfg.EmbedRoute()
	if base != "https://openrouter.ai/api" {
		t.Fatalf("OpenRouter option resolved to %q: embeddings followed the chat LLM's base", base)
	}
	if key != "sk-or-v1-test" || model != "qwen/qwen3-embedding-8b" {
		t.Errorf("route = (%q, %q), want the OpenRouter credential and the chosen model", key, model)
	}
}
