package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

func TestRunTaskHappyPath(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{{
		Content: "task done",
		Usage:   llm.TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}}}
	res, err := RunTask(context.Background(), RunTaskDeps{
		LLM:   client,
		Model: "test-model",
	}, Task{
		SystemPrompt: "system",
		Prompt:       "do something",
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if res.Content != "task done" {
		t.Fatalf("Content = %q", res.Content)
	}
	if res.LLMCalls != 1 || res.ToolCalls != 0 {
		t.Fatalf("stats = llm:%d tools:%d", res.LLMCalls, res.ToolCalls)
	}
	if res.Tokens.TotalTokens != 3 {
		t.Fatalf("TotalTokens = %d", res.Tokens.TotalTokens)
	}
}

func TestRunTaskMaxIterationsHitFallbackMessage(t *testing.T) {
	// One tool call response, no final LLM response — MaxIterations=1 forces
	// the fallback message with the last tool result appended.
	client := &fakeLLM{resps: []llm.Response{
		{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "call_1", Name: "lookup"}},
		},
	}}
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "lookup", result: "last result"})

	res, err := RunTask(context.Background(), RunTaskDeps{
		LLM:           client,
		Tools:         reg,
		MaxIterations: 1,
	}, Task{
		Prompt:        "loop",
		ToolAllowlist: []string{"lookup"},
	})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if !strings.Contains(res.Content, "maximum iteration") || !strings.Contains(res.Content, "last result") {
		t.Fatalf("fallback content = %q", res.Content)
	}
}

func TestRunTaskRequiresPromptOrMessages(t *testing.T) {
	_, err := RunTask(context.Background(), RunTaskDeps{LLM: &fakeLLM{}}, Task{})
	if err == nil || !strings.Contains(err.Error(), "prompt or messages required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunTaskTimeoutWrapsContext(t *testing.T) {
	// LLM delay (200ms) longer than Timeout (10ms) — RunTask must cancel and
	// return an error well before the outer context would expire.
	client := &fakeLLM{delays: []time.Duration{200 * time.Millisecond}}
	start := time.Now()
	_, err := RunTask(context.Background(), RunTaskDeps{
		LLM:     client,
		Timeout: 10 * time.Millisecond,
	}, Task{Prompt: "slow"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("RunTask took %s, want < 150ms (Timeout=10ms)", elapsed)
	}
}
