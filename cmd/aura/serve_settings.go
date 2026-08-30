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

func (r *primaryLLMRouteReloader) Prepare(ctx context.Context, overrides map[string]string) (func(), error) {
	cfg, err := r.resolve(overrides)
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
	case "AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT":
		return strconv.Itoa(cfg.CompactionTriggerPercent), true
	case "OPENROUTER_API_KEY":
		// Read only for has_value: the Settings GET never puts a secret on the wire.
		return cfg.APIKey, true
	case "AURA_LOOP_MAX_STEPS":
		return optionalLoopSetting(cfg.LoopMaxSteps)
	case "AURA_LOOP_MAX_WALLCLOCK_SEC":
		return optionalLoopSetting(cfg.LoopMaxWallclockSec)
	default:
		return "", false
	}
}

// optionalLoopSetting reports a loop-budget field only when the profile pins it;
// zero means the run falls through to AURA_LOOP_* env / builtin default, which
// the Settings GET then shows from the process env like any unpinned key.
func optionalLoopSetting(n int) (string, bool) {
	if n <= 0 {
		return "", false
	}
	return strconv.Itoa(n), true
}

func (r *primaryLLMRouteReloader) resolve(overrides map[string]string) (llm.Config, error) {
	cfg := r.fallback
	if r.runtime != nil {
		// Non-profile settings come from the active config. Every hot profile key
		// (route, limits, compaction trigger, API key, loop budget — amendment #188)
		// starts from the pre-overlay deployment fallback captured for DELETE and is
		// then re-applied from the persisted overrides below, so an absent row means
		// "back to the boot value", never "keep whatever was live".
		cfg = r.runtime.Snapshot().Config
		cfg.Provider = r.fallback.Provider
		cfg.BaseURL = r.fallback.BaseURL
		cfg.Model = r.fallback.Model
		cfg.MaxTokens = r.fallback.MaxTokens
		cfg.ContextWindow = r.fallback.ContextWindow
		cfg.ContextWindowConfigured = r.fallback.ContextWindowConfigured
		cfg.MaxOutputTokens = r.fallback.MaxOutputTokens
		cfg.MaxOutputTokensConfigured = r.fallback.MaxOutputTokensConfigured
		cfg.CompactionTriggerPercent = r.fallback.CompactionTriggerPercent
		cfg.APIKey = r.fallback.APIKey
		cfg.LoopMaxSteps = r.fallback.LoopMaxSteps
		cfg.LoopMaxWallclockSec = r.fallback.LoopMaxWallclockSec
	}
	cfg.Headers = maps.Clone(cfg.Headers)
	cfg.Prices = maps.Clone(r.fallback.Prices)
	if value, ok := overrides["OPENROUTER_API_KEY"]; ok {
		cfg.APIKey = strings.TrimSpace(value)
	}
	if value, ok := overrides["AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT"]; ok {
		parsed, err := percentLLMSetting("AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT", value)
		if err != nil {
			return llm.Config{}, err
		}
		cfg.CompactionTriggerPercent = parsed
	}
	if value, ok := overrides["AURA_LOOP_MAX_STEPS"]; ok {
		parsed, err := positiveLLMSetting("AURA_LOOP_MAX_STEPS", value)
		if err != nil {
			return llm.Config{}, err
		}
		cfg.LoopMaxSteps = parsed
	}
	if value, ok := overrides["AURA_LOOP_MAX_WALLCLOCK_SEC"]; ok {
		parsed, err := positiveLLMSetting("AURA_LOOP_MAX_WALLCLOCK_SEC", value)
		if err != nil {
			return llm.Config{}, err
		}
		cfg.LoopMaxWallclockSec = parsed
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

// percentLLMSetting accepts 0..100 inclusive: 0 and 100 both switch the early
// compaction trigger off (context_budget.go), so they are valid, not errors.
func percentLLMSetting(key, value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 || parsed > 100 {
		return 0, fmt.Errorf("%s must be a percentage between 0 and 100", key)
	}
	return parsed, nil
}
