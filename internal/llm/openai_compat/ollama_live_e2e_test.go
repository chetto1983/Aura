//go:build live_e2e

package openai_compat

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

func ollamaLiveConfig(t *testing.T) llm.Config {
	t.Helper()
	if os.Getenv("AURA_OLLAMA_LIVE") != "1" {
		t.Skip("set AURA_OLLAMA_LIVE=1 to run the operator-authorized Ollama cloud test")
	}
	baseURL := os.Getenv("AURA_OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	model := os.Getenv("AURA_OLLAMA_MODEL")
	if model == "" {
		model = "gemma4:31b-cloud"
	}
	return llm.Config{
		Provider: "ollama", BaseURL: baseURL, Model: model,
		Temperature: 0, MaxTokens: 256, ContextWindow: 1_000_000, MaxOutputTokens: 32768,
		TotalTimeoutSec: 120, ConnectTimeoutSec: 10, StreamIdleTimeoutSec: 120,
	}
}

func drainOllamaLive(t *testing.T, cfg llm.Config, request llm.Request) (string, string, []llm.ToolCall, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	stream, err := New(cfg).Stream(ctx, request)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	var reasoning strings.Builder
	var calls []llm.ToolCall
	finish := ""
	for chunk := range stream {
		if chunk.Err != nil {
			t.Fatalf("stream chunk: %v", chunk.Err)
		}
		text.WriteString(chunk.Text)
		reasoning.WriteString(chunk.Reasoning)
		if chunk.ToolCall != nil {
			calls = append(calls, *chunk.ToolCall)
		}
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
	}
	return text.String(), reasoning.String(), calls, finish
}

func TestOllamaLiveStreamingCompletion(t *testing.T) {
	cfg := ollamaLiveConfig(t)
	text, _, calls, finish := drainOllamaLive(t, cfg, llm.Request{
		Model: cfg.Model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "Return only the exact token requested by the user."},
			{Role: llm.RoleUser, Content: "Return exactly AURA_OLLAMA_STREAM_OK"},
		},
		Temperature: 0,
		MaxTokens:   64,
	})
	if strings.TrimSpace(text) != "AURA_OLLAMA_STREAM_OK" || len(calls) != 0 || finish != "stop" {
		t.Fatalf("completion = text %q, calls %d, finish %q", text, len(calls), finish)
	}
}

func TestOllamaLiveStreamingReasoning(t *testing.T) {
	cfg := ollamaLiveConfig(t)
	text, reasoning, calls, finish := drainOllamaLive(t, cfg, llm.Request{
		Model: cfg.Model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "Think before answering, then return only the exact token requested."},
			{Role: llm.RoleUser, Content: "Return exactly AURA_OLLAMA_REASONING_OK"},
		},
		Temperature: 0,
		MaxTokens:   256,
		Reasoning:   llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh},
	})
	if strings.TrimSpace(text) != "AURA_OLLAMA_REASONING_OK" || strings.TrimSpace(reasoning) == "" || len(calls) != 0 || finish != "stop" {
		t.Fatalf("reasoning stream = text %q, reasoning chars %d, calls %d, finish %q", text, len(reasoning), len(calls), finish)
	}
}

func TestOllamaLiveProfileDiscovery(t *testing.T) {
	cfg := ollamaLiveConfig(t)
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatalf("ResolveModelProfile: %v", err)
	}
	if cfg.ContextWindow != 262144 {
		t.Fatalf("context window = %d, want 262144", cfg.ContextWindow)
	}
	if cfg.CostStatus != llm.CostStatusSubscriptionIncluded {
		t.Fatalf("cost status = %q, want subscription-included", cfg.CostStatus)
	}
	if _, priced := cfg.Prices[cfg.Model]; priced {
		t.Fatal("Ollama cloud profile exposed a numeric token price")
	}
}

func TestOllamaLiveStreamingToolCall(t *testing.T) {
	cfg := ollamaLiveConfig(t)
	var tool llm.ToolDef
	tool.Type = "function"
	tool.Function.Name = "report_status"
	tool.Function.Description = "Report the exact requested status."
	tool.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}},"required":["status"]}`)

	text, _, calls, finish := drainOllamaLive(t, cfg, llm.Request{
		Model: cfg.Model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "Call report_status exactly once. Do not answer with text."},
			{Role: llm.RoleUser, Content: "Report status AURA_OLLAMA_TOOL_OK."},
		},
		Tools:       []llm.ToolDef{tool},
		Temperature: 0,
		MaxTokens:   128,
	})
	if strings.TrimSpace(text) != "" || len(calls) != 1 || calls[0].Function.Name != "report_status" {
		t.Fatalf("tool stream = text %q, calls %+v, finish %q", text, calls, finish)
	}
	var arguments struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &arguments); err != nil {
		t.Fatalf("tool arguments: %v", err)
	}
	if arguments.Status != "AURA_OLLAMA_TOOL_OK" || finish != "tool_calls" {
		t.Fatalf("tool arguments = %+v, finish %q", arguments, finish)
	}
}
