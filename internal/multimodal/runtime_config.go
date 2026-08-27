package multimodal

import "github.com/chetto1983/aura/internal/config"

// VisionConfigFrom projects Aura's canonical runtime settings onto the shared client.
func VisionConfigFrom(cfg *config.Config) VisionConfig {
	return VisionConfig{
		VisionCloud:       cfg.VisionCloud,
		Model:             cfg.LLM.Model,
		LocalBaseURL:      cfg.MultimodalBaseURL,
		LocalModel:        cfg.MultimodalModel,
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
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
