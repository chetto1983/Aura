package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

const (
	llmNotConfiguredCode = "llm_not_configured"
	llmNotConfiguredHint = "set OPENROUTER_API_KEY in .env or the environment, then retry"
)

type llmNotConfiguredClient struct{}

type llmNotConfiguredError struct{}

func newLLMClient(cfg llm.Config) llm.Client {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return llmNotConfiguredClient{}
	}
	return openai_compat.New(cfg)
}

func (llmNotConfiguredClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, llmNotConfiguredError{}
}

func (llmNotConfiguredError) Error() string {
	payload := struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}{
		Error: llmNotConfiguredCode,
		Hint:  llmNotConfiguredHint,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"llm_not_configured","hint":"set OPENROUTER_API_KEY in .env or the environment, then retry"}`
	}
	return string(data)
}
