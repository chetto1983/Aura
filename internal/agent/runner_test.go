package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
)

type fakeLLM struct {
	mu       sync.Mutex
	requests []llm.Request
	resps    []llm.Response
	errs     []error
	err      error
}

func (f *fakeLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return llm.Response{}, err
		}
	}
	if f.err != nil {
		return llm.Response{}, f.err
	}
	if len(f.resps) == 0 {
		return llm.Response{Content: "done"}, nil
	}
	resp := f.resps[0]
	f.resps = f.resps[1:]
	return resp, nil
}

func (f *fakeLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	return nil, errors.New("stream not implemented")
}

func (f *fakeLLM) lastRequest(t *testing.T) llm.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("no requests captured")
	}
	return f.requests[len(f.requests)-1]
}

type fakeTool struct {
	name   string
	result string
	err    error

	mu     sync.Mutex
	calls  int
	userID string
	delay  time.Duration
}

func (t *fakeTool) Name() string { return t.name }

func (t *fakeTool) Description() string { return "fake tool " + t.name }

func (t *fakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *fakeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	t.userID = tools.UserIDFromContext(ctx)
	if t.err != nil {
		return "", t.err
	}
	return t.result, nil
}

func (t *fakeTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func (t *fakeTool) seenUserID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.userID
}

func TestRunnerTextOnly(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{{
		Content: "final answer",
		Usage:   llm.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
	}}}
	runner, err := NewRunner(Config{LLM: client, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	res, err := runner.Run(context.Background(), Task{
		SystemPrompt: "system",
		Prompt:       "do work",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Content != "final answer" {
		t.Fatalf("Content = %q", res.Content)
	}
	if res.LLMCalls != 1 || res.ToolCalls != 0 {
		t.Fatalf("stats = llm:%d tools:%d", res.LLMCalls, res.ToolCalls)
	}
	if res.Tokens.TotalTokens != 5 {
		t.Fatalf("TotalTokens = %d", res.Tokens.TotalTokens)
	}

	req := client.lastRequest(t)
	if req.Model != "test-model" {
		t.Fatalf("Model = %q", req.Model)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("text-only runner exposed tools: %+v", req.Tools)
	}
}

func TestRunnerToolLoop(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{
		{
			Content:      "checking",
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{{
				ID:        "call_1",
				Name:      "lookup",
				Arguments: map[string]any{"q": "aura"},
			}},
		},
		{Content: "used the tool"},
	}}
	reg := tools.NewRegistry(nil)
	lookup := &fakeTool{name: "lookup", result: "tool result"}
	reg.Register(lookup)

	runner, err := NewRunner(Config{LLM: client, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := runner.Run(context.Background(), Task{
		Prompt:        "do work",
		ToolAllowlist: []string{"lookup"},
		UserID:        "12345",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Content != "used the tool" {
		t.Fatalf("Content = %q", res.Content)
	}
	if lookup.callCount() != 1 {
		t.Fatalf("tool calls = %d", lookup.callCount())
	}
	if lookup.seenUserID() != "12345" {
		t.Fatalf("tool user id = %q", lookup.seenUserID())
	}
	if res.LLMCalls != 2 || res.ToolCalls != 1 {
		t.Fatalf("stats = llm:%d tools:%d", res.LLMCalls, res.ToolCalls)
	}
	if len(res.Messages) < 4 || res.Messages[2].Role != "tool" || res.Messages[2].Content != "tool result" {
		t.Fatalf("tool result not appended in order: %+v", res.Messages)
	}
}

func TestRunnerToolTimeout(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{
		{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "call_slow", Name: "slow"}},
		},
		{Content: "handled timeout"},
	}}
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "slow", result: "late", delay: 50 * time.Millisecond})

	runner, err := NewRunner(Config{
		LLM:         client,
		Tools:       reg,
		ToolTimeout: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := runner.Run(context.Background(), Task{
		Prompt:        "run slow tool",
		ToolAllowlist: []string{"slow"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Messages[2].Content, "context deadline exceeded") {
		t.Fatalf("tool timeout result = %q", res.Messages[2].Content)
	}
}

func TestRunnerToolBudgetForcesFinalTurn(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{
		{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{
				{ID: "call_1", Name: "lookup"},
				{ID: "call_2", Name: "lookup"},
				{ID: "call_3", Name: "lookup"},
			},
		},
		{Content: "final from compact evidence"},
	}}
	reg := tools.NewRegistry(nil)
	lookup := &fakeTool{name: "lookup", result: "0123456789abcdefghijklmnopqrstuvwxyz"}
	reg.Register(lookup)

	runner, err := NewRunner(Config{LLM: client, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := runner.Run(context.Background(), Task{
		Prompt:             "research",
		ToolAllowlist:      []string{"lookup"},
		MaxToolCalls:       2,
		MaxToolResultChars: 12,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Content != "final from compact evidence" {
		t.Fatalf("Content = %q", res.Content)
	}
	if lookup.callCount() != 2 || res.ToolCalls != 2 {
		t.Fatalf("tool calls executed=%d recorded=%d", lookup.callCount(), res.ToolCalls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("llm requests = %d", len(client.requests))
	}
	if len(client.requests[1].Tools) != 0 {
		t.Fatalf("finalizing turn exposed tools: %+v", client.requests[1].Tools)
	}
	if !strings.Contains(res.Messages[len(res.Messages)-2].Content, "Tool budget reached") {
		t.Fatalf("missing budget instruction in messages: %+v", res.Messages)
	}
	if got := res.Messages[2].Content; strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("tool result was not clipped: %q", got)
	}
}

func TestRunnerCompletesWithPartialOnDeadlineAfterTools(t *testing.T) {
	client := &fakeLLM{
		resps: []llm.Response{{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "call_1", Name: "lookup"}},
		}},
		errs: []error{nil, context.DeadlineExceeded},
	}
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "lookup", result: "evidence"})

	runner, err := NewRunner(Config{LLM: client, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := runner.Run(context.Background(), Task{
		Prompt:             "research",
		ToolAllowlist:      []string{"lookup"},
		CompleteOnDeadline: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Content, "interrupted") || !strings.Contains(res.Content, "evidence") {
		t.Fatalf("partial content = %q", res.Content)
	}
	if res.LLMCalls != 2 || res.ToolCalls != 1 {
		t.Fatalf("stats = llm:%d tools:%d", res.LLMCalls, res.ToolCalls)
	}
}

func TestRunnerFiltersToolDefinitionsAndBlocksDisallowedCall(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{
		{
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_blocked",
				Name: "dangerous",
			}},
		},
		{Content: "blocked"},
	}}
	reg := tools.NewRegistry(nil)
	allowed := &fakeTool{name: "safe", result: "ok"}
	dangerous := &fakeTool{name: "dangerous", result: "should not run"}
	reg.Register(allowed)
	reg.Register(dangerous)

	runner, err := NewRunner(Config{LLM: client, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := runner.Run(context.Background(), Task{
		Prompt:        "try",
		ToolAllowlist: []string{"safe"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	firstReq := client.requests[0]
	if len(firstReq.Tools) != 1 || firstReq.Tools[0].Name != "safe" {
		t.Fatalf("allowed tools = %+v", firstReq.Tools)
	}
	if dangerous.callCount() != 0 {
		t.Fatalf("disallowed tool executed %d time(s)", dangerous.callCount())
	}
	if !strings.Contains(res.Messages[2].Content, "not allowed") {
		t.Fatalf("blocked tool result = %q", res.Messages[2].Content)
	}
}

func TestRunnerMaxIterationsFallback(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{
		{
			HasToolCalls: true,
			ToolCalls:    []llm.ToolCall{{ID: "call_1", Name: "lookup"}},
		},
	}}
	reg := tools.NewRegistry(nil)
	reg.Register(&fakeTool{name: "lookup", result: "last result"})

	runner, err := NewRunner(Config{LLM: client, Tools: reg, MaxIterations: 1})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := runner.Run(context.Background(), Task{
		Prompt:        "loop",
		ToolAllowlist: []string{"lookup"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Content, "maximum iteration") || !strings.Contains(res.Content, "last result") {
		t.Fatalf("fallback content = %q", res.Content)
	}
}

func TestRunnerRequiresPromptOrMessages(t *testing.T) {
	runner, err := NewRunner(Config{LLM: &fakeLLM{}})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_, err = runner.Run(context.Background(), Task{})
	if err == nil || !strings.Contains(err.Error(), "prompt or messages required") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRunnerRequiresLLM(t *testing.T) {
	if _, err := NewRunner(Config{}); err == nil {
		t.Fatal("expected missing LLM error")
	}
}
