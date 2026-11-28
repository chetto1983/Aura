package main

import (
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/anthropic"
	"github.com/chetto1983/aura/internal/llm/chatgpt"
)

func TestPrimaryImageReaderFollowsTheRoute(t *testing.T) {
	cfg := &config.Config{SkillsDir: filepath.Join(t.TempDir(), "skills"), AuthulaSecret: "00000000000000000000000000000000000000000000000000000000000000a1"}
	plan := newChatGPTPlan(cfg)
	if plan == nil {
		t.Fatal("connection service not created")
	}
	creds, _ := storedCredentials(map[string]string{})
	chatGPT := llm.Config{Provider: llm.ChatGPTProvider, BaseURL: llm.ChatGPTBaseURL, Model: "gpt-6.1-sol"}
	claude := llm.Config{Provider: llm.AnthropicProvider, BaseURL: llm.AnthropicBaseURL, Model: "claude-sonnet-5-5"}
	if _, ok := primaryImageReader(chatGPT, plan, creds.AnthropicKey).(*chatgpt.Client); !ok {
		t.Fatal("no ChatGPT reader on the ChatGPT route")
	}
	if _, ok := primaryImageReader(claude, nil, creds.AnthropicKey).(*anthropic.Client); !ok {
		t.Fatal("no Claude reader on the anthropic route")
	}
	if reader := primaryImageReader(llm.Config{Provider: "openrouter", Model: "z-ai/glm-5.3"}, plan, creds.AnthropicKey); reader != nil {
		t.Fatalf("image reader %v on OpenRouter", reader)
	}
	if reader := primaryImageReader(chatGPT, nil, creds.AnthropicKey); reader != nil {
		t.Fatalf("image reader %v without a connection service", reader)
	}
}
