package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/chatgpt"
)

func newChatGPTPlan(cfg *config.Config) *chatgptplan.Service {
	if cfg == nil || cfg.AuthulaSecret == "" || cfg.SkillsDir == "" {
		return nil
	}
	service, err := chatgptplan.New(filepath.Dir(cfg.SkillsDir), cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("ChatGPT connection unavailable")
		return nil
	}
	return service
}

func chatGPTTokenSourceOrNil(service *chatgptplan.Service) chatGPTTokenSource {
	if service == nil {
		return nil
	}
	return service
}

func validateChatGPTModel(ctx context.Context, cfg *llm.Config, tokens chatGPTTokenSource) error {
	if cfg.Provider != llm.ChatGPTProvider {
		return nil
	}
	if tokens == nil {
		return errors.New("connect ChatGPT in Settings before selecting a model")
	}
	models, err := chatgpt.FetchModels(ctx, tokens)
	if err != nil {
		return errors.New("unable to load your ChatGPT models; reconnect ChatGPT in Settings")
	}
	return applyChatGPTCatalogModel(cfg, models)
}

func applyChatGPTCatalogModel(cfg *llm.Config, models []llm.ModelCatalogEntry) error {
	for _, model := range models {
		if model.ID != cfg.Model {
			continue
		}
		candidate := *cfg
		if model.ContextWindow > 0 && !candidate.ContextWindowConfigured {
			candidate.ContextWindow = model.ContextWindow
		}
		if !candidate.MaxOutputTokensConfigured {
			candidate.MaxOutputTokens = llm.DerivedMaxOutputTokens(candidate.ContextWindow)
		}
		candidate.SupportedReasoningEfforts = slices.Clone(model.SupportedReasoningEfforts)
		candidate.ReasoningMandatory = model.ReasoningMandatory
		if err := candidate.Validate(); err != nil {
			return err
		}
		*cfg = candidate
		return nil
	}
	return errors.New("select a model available to your ChatGPT account")
}
