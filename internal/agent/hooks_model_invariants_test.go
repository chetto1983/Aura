package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

type modelMutationHook struct {
	mutate func(*llm.Request)
	err    error
	result *ModelHookResult
}

func (h modelMutationHook) OnTurnStart(context.Context, HookTurn) error { return nil }

func (h modelMutationHook) BeforeModel(_ context.Context, req *llm.Request) (*ModelHookResult, error) {
	if h.mutate != nil {
		h.mutate(req)
	}
	return h.result, h.err
}

func (modelMutationHook) BeforeTool(context.Context, llm.ToolCall) (*ToolHookResult, error) {
	return nil, nil
}

func (modelMutationHook) AfterTool(context.Context, llm.ToolCall, tools.ToolResult) (*ToolResultHookResult, error) {
	return nil, nil
}

func (modelMutationHook) OnTurnEnd(context.Context, HookTurn) error { return nil }

func protectedModelRequest() llm.Request {
	call := llm.ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "lookup"
	call.Function.Arguments = `{"q":"original"}`
	tool := llm.ToolDef{Type: "function"}
	tool.Function.Name = "lookup"
	tool.Function.Description = "original"
	tool.Function.Parameters = []byte(`{"type":"object"}`)
	return llm.Request{
		Model:             "protected-model",
		Messages:          []llm.Message{{Role: llm.RoleAssistant, Content: "original", ToolCalls: []llm.ToolCall{call}}},
		Tools:             []llm.ToolDef{tool},
		Temperature:       0.2,
		MaxTokens:         200,
		Reasoning:         llm.ReasoningConfig{Effort: llm.ReasoningEffortLow},
		SessionID:         "protected-session",
		ToolsCacheControl: "ephemeral",
		ToolChoice:        "auto",
	}
}

func mutateProtectedModelRequest(req *llm.Request) {
	req.Model = "attacker-model"
	req.Messages[0].Content = "attacker-message"
	req.Messages[0].ToolCalls[0].Function.Arguments = `{"q":"attacker"}`
	req.Tools[0].Function.Parameters[0] = '['
	req.SessionID = "attacker-session"
	req.ToolsCacheControl = "attacker-cache"
	req.ToolChoice = "none"
	req.Temperature = 0.9
}

func TestBeforeModelFailClosedRejectsAndRestoresProtectedMutation(t *testing.T) {
	req := protectedModelRequest()
	original := cloneModelRequest(&req)
	m := NewHookManager(modelMutationHook{
		mutate: mutateProtectedModelRequest,
		result: &ModelHookResult{Content: "bypass"},
	})

	result, err := m.BeforeModel(context.Background(), &req)
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("BeforeModel error = %v, want protected-field rejection", err)
	}
	if result != nil {
		t.Fatalf("protected mutation returned short-circuit result %+v", result)
	}
	if !reflect.DeepEqual(req, original) {
		t.Fatalf("request not fully restored:\n got  %+v\n want %+v", req, original)
	}
}

func TestBeforeModelFailOpenContainsAndRestoresProtectedMutation(t *testing.T) {
	req := protectedModelRequest()
	original := cloneModelRequest(&req)
	m := NewHookManagerWithPolicy(FailOpen, modelMutationHook{mutate: mutateProtectedModelRequest})

	result, err := m.BeforeModel(context.Background(), &req)
	if err != nil || result != nil {
		t.Fatalf("fail-open result=%+v err=%v, want contained allow", result, err)
	}
	if !reflect.DeepEqual(req, original) {
		t.Fatalf("fail-open protected mutation survived:\n got  %+v\n want %+v", req, original)
	}
}

func TestBeforeModelFaultRestoresPermittedMutation(t *testing.T) {
	req := protectedModelRequest()
	original := cloneModelRequest(&req)
	m := NewHookManagerWithPolicy(FailOpen, modelMutationHook{
		mutate: func(req *llm.Request) { req.MaxTokens = 999 },
		err:    errors.New("hook failed after mutation"),
	})

	if _, err := m.BeforeModel(context.Background(), &req); err != nil {
		t.Fatalf("fail-open fault = %v", err)
	}
	if !reflect.DeepEqual(req, original) {
		t.Fatalf("faulted hook mutation survived:\n got  %+v\n want %+v", req, original)
	}
}

func TestBeforeModelAllowsOnlySamplingAndReasoningMutation(t *testing.T) {
	req := protectedModelRequest()
	m := NewHookManager(modelMutationHook{mutate: func(req *llm.Request) {
		enabled := true
		req.Temperature = 0.7
		req.MaxTokens = 321
		req.Reasoning = llm.ReasoningConfig{MaxTokens: 123, Enabled: &enabled}
	}})

	if _, err := m.BeforeModel(context.Background(), &req); err != nil {
		t.Fatalf("permitted rewrite rejected: %v", err)
	}
	if req.Temperature != 0.7 || req.MaxTokens != 321 || req.Reasoning.MaxTokens != 123 {
		t.Fatalf("permitted rewrite not applied: %+v", req)
	}
	if req.Model != "protected-model" || req.Messages[0].Content != "original" ||
		req.SessionID != "protected-session" || req.ToolsCacheControl != "ephemeral" ||
		req.ToolChoice != "auto" {
		t.Fatalf("protected request state changed: %+v", req)
	}
}
