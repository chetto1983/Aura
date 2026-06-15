package agent

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// HookTurn carries immutable turn identity into lifecycle hooks.
type HookTurn struct {
	AgentName string
	ThreadID  string
	Branch    string
	RequestID string
	Reason    string
}

// ModelHookResult lets a hook short-circuit a model request.
type ModelHookResult struct {
	Content      string
	FinishReason string
	ToolCalls    []llm.ToolCall
	Usage        llm.Usage
}

// ToolHookResult lets a hook rewrite or veto a tool call. A non-nil Result
// skips execution and is threaded as the tool result.
type ToolHookResult struct {
	Call   *llm.ToolCall
	Result *tools.ToolResult
}

// ToolResultHookResult lets a hook rewrite the result before it enters history.
type ToolResultHookResult struct {
	Result *tools.ToolResult
}

// Hook is the in-process plugin surface for one LlmAgent turn.
type Hook interface {
	OnTurnStart(context.Context, HookTurn) error
	BeforeModel(context.Context, *llm.Request) (*ModelHookResult, error)
	BeforeTool(context.Context, llm.ToolCall) (*ToolHookResult, error)
	AfterTool(context.Context, llm.ToolCall, tools.ToolResult) (*ToolResultHookResult, error)
	OnTurnEnd(context.Context, HookTurn) error
}

// HookManager composes hooks in registration order. For result-producing hook
// points, the first non-nil result wins.
type HookManager struct {
	hooks []Hook
}

// NewHookManager builds a manager, skipping nil hooks.
func NewHookManager(hooks ...Hook) *HookManager {
	m := &HookManager{}
	for _, h := range hooks {
		m.Register(h)
	}
	return m
}

// Register appends h when non-nil.
func (m *HookManager) Register(h Hook) {
	if m == nil || h == nil {
		return
	}
	m.hooks = append(m.hooks, h)
}

// OnTurnStart runs registered start hooks in order.
func (m *HookManager) OnTurnStart(ctx context.Context, turn HookTurn) error {
	if m == nil {
		return nil
	}
	for _, h := range m.hooks {
		if err := h.OnTurnStart(ctx, turn); err != nil {
			return err
		}
	}
	return nil
}

// BeforeModel gives registered hooks a chance to rewrite or answer a model request.
func (m *HookManager) BeforeModel(ctx context.Context, req *llm.Request) (*ModelHookResult, error) {
	if m == nil {
		return nil, nil
	}
	for _, h := range m.hooks {
		res, err := h.BeforeModel(ctx, req)
		if err != nil || res != nil {
			return res, err
		}
	}
	return nil, nil
}

// BeforeTool gives registered hooks a chance to rewrite or veto a tool call.
func (m *HookManager) BeforeTool(ctx context.Context, call llm.ToolCall) (*ToolHookResult, error) {
	if m == nil {
		return nil, nil
	}
	for _, h := range m.hooks {
		res, err := h.BeforeTool(ctx, call)
		if err != nil || res != nil {
			return res, err
		}
	}
	return nil, nil
}

// AfterTool gives registered hooks a chance to rewrite a completed tool result.
func (m *HookManager) AfterTool(ctx context.Context, call llm.ToolCall, res tools.ToolResult) (*ToolResultHookResult, error) {
	if m == nil {
		return nil, nil
	}
	for _, h := range m.hooks {
		next, err := h.AfterTool(ctx, call, res)
		if err != nil || next != nil {
			return next, err
		}
	}
	return nil, nil
}

// OnTurnEnd runs registered end hooks in order.
func (m *HookManager) OnTurnEnd(ctx context.Context, turn HookTurn) error {
	if m == nil {
		return nil
	}
	for _, h := range m.hooks {
		if err := h.OnTurnEnd(ctx, turn); err != nil {
			return err
		}
	}
	return nil
}

func (a *LlmAgent) hookTurn(ic InvocationContext, reason string) HookTurn {
	return HookTurn{
		AgentName: a.name,
		ThreadID:  a.sessionID,
		Branch:    ic.Branch,
		RequestID: ic.RequestID.String(),
		Reason:    reason,
	}
}
