package config

import (
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/envutil"
)

// DefaultEmbedDimensions is the NATIVE width of the repo's default embedding sidecar
// (EmbeddingGemma-300M). The width is deliberately not restated in prose elsewhere: it
// moved 384 -> 768 -> 1024 -> 768 while comments went on asserting the old number. A
// deployment may set a smaller AURA_EMBED_DIMENSIONS, but only together with vector
// indexes created at that same width.
const DefaultEmbedDimensions = 768

// EmbedConfig is the embedding-sidecar wiring: an OpenAI-compatible endpoint, the
// contract width every stored vector is narrowed to, and the model that decides
// whether the endpoint is the local sidecar or a hosted one (see EmbedRoute).
//
// It lives here rather than in an owning subsystem package because there is no
// longer one: the embedder is read by the reasoning classifier, the doctor, the
// adaptive controls and memory alike.
// The four fields are separate on purpose, exactly as multimodal.TTSConfig and
// STTConfig already keep LocalBaseURL, CloudModel and OpenRouterBaseURL apart. Embed
// used to fold the local and the cloud base into ONE field and guess which was meant
// from whether it looked like loopback. Measured 2026-09-21 on a live appliance, that
// guess is wrong on the shipped product: compose.yaml gives the daemon
// http://aura-llama-embed:8081, a Compose DNS name and not loopback, so setting a cloud
// model sent the cloud model NAME and the OpenRouter key to the LOCAL sidecar. llama.cpp
// ignores both -- three requests (local model, cloud model with an Authorization header,
// and an invented model name) returned the byte-identical EmbeddingGemma vector, with no
// error on any of them. The operator gets local vectors believing they are cloud ones,
// and every healthcheck stays green. Keeping the two bases apart makes that state
// unrepresentable rather than merely detectable.
type EmbedConfig struct {
	Revision    string // AURA_EMBED_REVISION: immutable deployed model revision.
	Fingerprint string // AURA_EMBED_FINGERPRINT: SHA-256 of the exact deployed artifact.
	BaseURL     string // AURA_EMBED_BASE_URL — the LOCAL sidecar, and only ever that
	Dimensions  int    // AURA_EMBED_DIMENSIONS — contract width; a narrower response is an error
	// AURA_EMBED_MODEL — the ONE switch, as AURA_STT_CLOUD_MODEL is for speech. Empty means
	// the local sidecar; set means the cloud route. The cockpit writes it from a picker over
	// the models OpenRouter publishes, so it is a choice rather than a typed name.
	CloudModel string
	// AURA_EMBED_CLOUD_BASE_URL — optional, for an OpenAI-compatible embedder that is not
	// OpenRouter. Empty means OpenRouter itself (never the chat LLM's base), which is what
	// makes the common case a single setting. It must NOT carry a trailing /v1: this client
	// appends /v1/embeddings, unlike the STT and TTS clients that append /audio/… to a base
	// that already has it.
	CloudBaseURL string
}

// loadEmbed reads the embedding wiring from the environment. It lives beside EmbedConfig
// rather than in loadBase so the four fields and the rules that keep them apart are read in
// one place.
func loadEmbed() EmbedConfig {
	return EmbedConfig{
		BaseURL:      envDefault("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
		Dimensions:   envutil.IntDefault("AURA_EMBED_DIMENSIONS", DefaultEmbedDimensions),
		CloudModel:   strings.TrimSpace(os.Getenv("AURA_EMBED_MODEL")),
		CloudBaseURL: strings.TrimSpace(os.Getenv("AURA_EMBED_CLOUD_BASE_URL")),
		Revision:     strings.TrimSpace(os.Getenv("AURA_EMBED_REVISION")),
		Fingerprint: strings.ToLower(strings.TrimSpace(
			os.Getenv("AURA_EMBED_FINGERPRINT"),
		)),
	}
}
