package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

// ---- shared test helpers (moved from deleted runner_test.go) ----------------

type fakeLLM struct {
	mu       sync.Mutex
	requests []llm.Request
	resps    []llm.Response
	errs     []error
	delays   []time.Duration
	err      error
}

type allowRunTaskAuthorizer struct{}

func (allowRunTaskAuthorizer) Authorize(_ context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return identity.AuthorizationDecision{
		ActorID:    params.ActorID,
		Capability: params.Capability,
		Resource:   params.Resource,
		Decision:   identity.DecisionAllow,
		Reason:     "test_grant",
	}, nil
}

func authorizedRunTaskContext() context.Context {
	ctx := identity.WithAuthorizer(context.Background(), allowRunTaskAuthorizer{})
	return identity.WithActorID(ctx, "actor:test-run-task")
}

func (f *fakeLLM) Send(ctx context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	var delay time.Duration
	if len(f.delays) > 0 {
		delay = f.delays[0]
		f.delays = f.delays[1:]
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			f.mu.Unlock()
			return llm.Response{}, err
		}
	}
	if f.err != nil {
		f.mu.Unlock()
		return llm.Response{}, f.err
	}
	if len(f.resps) == 0 {
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return llm.Response{}, ctx.Err()
			}
		}
		return llm.Response{Content: "done"}, nil
	}
	resp := f.resps[0]
	f.resps = f.resps[1:]
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return llm.Response{}, ctx.Err()
		}
	}
	return resp, nil
}

func (f *fakeLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	return nil, errors.New("stream not implemented")
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

func (t *fakeTool) Name() string        { return t.name }
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

func TestRunTaskHappyPath(t *testing.T) {
	client := &fakeLLM{resps: []llm.Response{{
		Content: "task done",
		Usage:   llm.TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}}}
	res, err := RunTask(authorizedRunTaskContext(), RunTaskDeps{
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

	res, err := RunTask(authorizedRunTaskContext(), RunTaskDeps{
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
