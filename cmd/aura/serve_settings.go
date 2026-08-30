package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
)

func wireSettingsProviders(server *agui.Server, chat *chatEnv) {
	server.SetSettingsStore(settings.NewStore(chat.pool))
	server.SetTelegramBotProbe(telegramGetMeProbe)
	server.SetLLMRuntime(chat.llmRuntime)
	server.SetLLMRouteReloader(&primaryLLMRouteReloader{
		fallback: chat.llmFallback,
		runtime:  chat.llmRuntime,
		server:   server,
	})
}

type primaryLLMRouteReloader struct {
	fallback llm.Config
	runtime  *llm.Runtime
	server   *agui.Server
}

func (r *primaryLLMRouteReloader) Prepare(ctx context.Context, overrides map[string]string, resetKeys []string) (func(), error) {
	cfg, err := r.resolve(overrides, resetKeys)
	if err != nil {
		return nil, err
	}
	if err := cfg.ResolveModelProfile(ctx); err != nil {
		return nil, err
	}
	client := newLLMClient(cfg)
	return func() {
		r.runtime.Replace(client, cfg)
		if r.server != nil {
			wireReasoningCapabilities(r.server, cfg)
			r.server.SetContextWindow(cfg.ContextWindow)
		}
		slog.Info("primary LLM profile updated", "provider", cfg.Provider, "model", cfg.Model)
	}, nil
}

func (r *primaryLLMRouteReloader) EffectiveValue(key string) (string, bool) {
	if r == nil || r.runtime == nil {
		return "", false
	}
	cfg := r.runtime.Snapshot().Config
	switch key {
	case "AURA_LLM_PROVIDER":
		return cfg.Provider, true
	case "AURA_LLM_BASE_URL":
		return cfg.BaseURL, true
	case "AURA_LLM_MODEL":
		return cfg.Model, true
	case "AURA_LLM_MAX_TOKENS":
		return strconv.Itoa(cfg.MaxTokens), true
	case "AURA_MODEL_CONTEXT_WINDOW":
		return strconv.Itoa(cfg.ContextWindow), true
	case "AURA_MODEL_MAX_OUTPUT_TOKENS":
		return strconv.Itoa(cfg.MaxOutputTokens), true
	default:
		return "", false
	}
}

func (r *primaryLLMRouteReloader) resolve(overrides map[string]string, resetKeys []string) (llm.Config, error) {
	cfg := r.fallback
	if r.runtime != nil {
		// Non-profile settings (most importantly the DB-overlaid API key) come
		// from the active config. Only absent hot keys revert to the pre-overlay
		// deployment fallback captured for DELETE.
		cfg = r.runtime.Snapshot().Config
		cfg.Provider = r.fallback.Provider
		cfg.BaseURL = r.fallback.BaseURL
		cfg.Model = r.fallback.Model
		cfg.MaxTokens = r.fallback.MaxTokens
		cfg.ContextWindow = r.fallback.ContextWindow
		cfg.ContextWindowConfigured = r.fallback.ContextWindowConfigured
		cfg.MaxOutputTokens = r.fallback.MaxOutputTokens
		cfg.MaxOutputTokensConfigured = r.fallback.MaxOutputTokensConfigured
	}
	cfg.Headers = maps.Clone(cfg.Headers)
	cfg.Prices = maps.Clone(r.fallback.Prices)
	for _, key := range resetKeys {
		switch key {
		case "AURA_MODEL_CONTEXT_WINDOW":
			cfg.ContextWindowConfigured = false
		case "AURA_MODEL_MAX_OUTPUT_TOKENS":
			cfg.MaxOutputTokensConfigured = false
		}
	}
	if value, ok := overrides["AURA_LLM_PROVIDER"]; ok {
		cfg.Provider = strings.TrimSpace(value)
	}
	if value, ok := overrides["AURA_LLM_BASE_URL"]; ok {
		cfg.BaseURL = strings.TrimSpace(value)
	}
	if value, ok := overrides["AURA_LLM_MODEL"]; ok {
		cfg.Model = strings.TrimSpace(value)
	}
	if value, ok := overrides["AURA_LLM_MAX_TOKENS"]; ok {
		parsed, err := positiveLLMSetting("AURA_LLM_MAX_TOKENS", value)
		if err != nil {
			return llm.Config{}, err
		}
		cfg.MaxTokens = parsed
	}
	if value, ok := overrides["AURA_MODEL_CONTEXT_WINDOW"]; ok {
		parsed, err := positiveLLMSetting("AURA_MODEL_CONTEXT_WINDOW", value)
		if err != nil {
			return llm.Config{}, err
		}
		cfg.ContextWindow = parsed
		cfg.ContextWindowConfigured = true
	}
	if value, ok := overrides["AURA_MODEL_MAX_OUTPUT_TOKENS"]; ok {
		parsed, err := positiveLLMSetting("AURA_MODEL_MAX_OUTPUT_TOKENS", value)
		if err != nil {
			return llm.Config{}, err
		}
		cfg.MaxOutputTokens = parsed
		cfg.MaxOutputTokensConfigured = true
	}
	if cfg.Provider != "openrouter" && cfg.Provider != "llamacpp" && cfg.Provider != "ollama" {
		return llm.Config{}, fmt.Errorf("primary LLM provider must be openrouter, llamacpp, or ollama")
	}
	if cfg.Model == "" {
		return llm.Config{}, fmt.Errorf("primary LLM model must not be empty")
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return llm.Config{}, fmt.Errorf("primary LLM base URL must be an absolute http(s) URL")
	}
	if err := cfg.Validate(); err != nil {
		return llm.Config{}, err
	}
	return cfg, nil
}

func positiveLLMSetting(key, value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}
