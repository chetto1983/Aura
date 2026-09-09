package multimodal

import (
	"context"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// PrimaryAcceptsImages asks the operator-selected model's own card (OpenRouter
// input_modalities) or capability probe (llama.cpp /props, Ollama /api/show) whether
// it reads images. z-ai/glm-5.3-flash advertises image while its plain sibling
// z-ai/glm-5.3 advertises text only, so this is a per-model fact no switch can track.
//
// A source that cannot answer — unknown backend, failed probe — is not permission:
// the answer is false and the local sidecar keeps the job.
func PrimaryAcceptsImages(ctx context.Context, source llm.ContentCapabilitySource) bool {
	if source == nil {
		return false
	}
	capabilities, ok := source.ContentCapabilities(ctx)
	if !ok {
		return false
	}
	return capabilities.Modalities["image"]
}

// VisionConfigFrom projects Aura's canonical runtime settings onto the shared client.
// primaryAcceptsImages is resolved by the caller (PrimaryAcceptsImages) so this stays
// a pure projection and the vision route issues no I/O to decide where to go.
func VisionConfigFrom(cfg *config.Config, primaryAcceptsImages bool) VisionConfig {
	return VisionConfig{
		PrimaryAcceptsImages: primaryAcceptsImages,
		Model:                cfg.LLM.Model,
		LocalBaseURL:         cfg.MultimodalBaseURL,
		LocalModel:           cfg.MultimodalModel,
		PrimaryBaseURL:       cfg.LLM.BaseURL,
		PrimaryAPIKey:        cfg.LLM.APIKey,
		TimeoutSec:           cfg.MultimodalTimeoutSec,
	}
}

// STTConfigFrom projects Aura's canonical runtime settings onto the shared client.
func STTConfigFrom(cfg *config.Config) STTConfig {
	return STTConfig{
		LocalBaseURL:      cfg.STTBaseURL,
		LocalModel:        cfg.STTModel,
		Language:          cfg.STTLanguage,
		CloudModel:        cfg.STTCloudModel,
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	}
}
