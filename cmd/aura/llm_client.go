package main

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
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
	if strings.TrimSpace(cfg.APIKey) == "" && !allowsKeylessLLMBaseURL(cfg.BaseURL) {
		return llmNotConfiguredClient{}
	}
	return openai_compat.New(cfg)
}

func allowsKeylessLLMBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "host.docker.internal" || strings.HasSuffix(host, ".local") {
		return true
	}
	switch host {
	case "ollama", "vllm", "llama", "llama-cpp", "aura-llm", "aura-vllm-chat", "aura-llama-chat", "aura-llama":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
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
