package main

import (
	"errors"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/anthropic"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/settings"
)

// newClaudeReader is a variable so a test can follow the house key into the reader without
// reaching Anthropic: the client only ever dials api.anthropic.com.
var newClaudeReader = func(cfg llm.Config, key anthropic.KeySource) multimodal.ImageReader {
	return anthropic.New(cfg, key)
}

// anthropicRoute is the Claude reader when the chat runs on Claude, and no reader off it. The
// house key lives in the settings store alone, so a process without one is an installation
// mistake: it fails and the file is retried, instead of reading as a deployment with no key
// and ending card-only for good.
func anthropicRoute(cfg *config.Config, creds settings.Credentials) (multimodal.ImageReader, error) {
	if cfg.LLM.Provider != llm.AnthropicProvider {
		return nil, nil
	}
	if creds.Store == nil {
		return nil, errors.New("a Claude route reads images with the house key in the settings store: AURA_DB_URL is not set")
	}
	return newClaudeReader(cfg.LLM, creds.AnthropicKey), nil
}
