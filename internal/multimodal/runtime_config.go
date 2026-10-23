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
// a pure projection and the vision route issues no I/O to decide where to go. primaryKey
// is the key the primary chat route is sent: the services key on OpenRouter, none elsewhere
// (settings.Credentials.PrimaryRouteKey).
func VisionConfigFrom(cfg *config.Config, primaryAcceptsImages bool, primaryKey KeyFunc) VisionConfig {
	return VisionConfig{
		PrimaryAcceptsImages: primaryAcceptsImages,
		Model:                cfg.LLM.Model,
		LocalBaseURL:         cfg.MultimodalBaseURL,
		LocalModel:           cfg.MultimodalModel,
		PrimaryBaseURL:       cfg.LLM.BaseURL,
		PrimaryKey:           primaryKey,
		TimeoutSec:           cfg.MultimodalTimeoutSec,
	}
}

// STTConfigFrom projects Aura's canonical runtime settings onto the shared client. The cloud
// leg is OpenRouter itself with servicesKey, whatever host the chat runs on: found by code
// reading on 2026-09-27, the cloud speech clients were built on the chat base URL and, with the
// chat on Ollama, would have posted /audio/* to Ollama with the OpenRouter key.
func STTConfigFrom(cfg *config.Config, servicesKey KeyFunc) STTConfig {
	return STTConfig{
		LocalBaseURL:      cfg.STTBaseURL,
		LocalModel:        cfg.STTModel,
		Language:          cfg.STTLanguage,
		CloudModel:        cfg.STTCloudModel,
		OpenRouterBaseURL: llm.DefaultBaseURL,
		OpenRouterKey:     servicesKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	}
}

// TTSConfigFrom projects Aura's canonical runtime settings onto the shared client, in the
// container format the caller delivers. The cloud leg is OpenRouter itself with servicesKey,
// as STTConfigFrom's is.
func TTSConfigFrom(cfg *config.Config, format string, servicesKey KeyFunc) TTSConfig {
	return TTSConfig{
		LocalBaseURL:      cfg.TTSBaseURL,
		Voice:             cfg.TTSVoice,
		Format:            format,
		CloudModel:        cfg.TTSModel,
		CloudVoice:        cfg.TTSCloudVoice,
		OpenRouterBaseURL: llm.DefaultBaseURL,
		OpenRouterKey:     servicesKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	}
}
