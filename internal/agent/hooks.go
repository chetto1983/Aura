package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// FailPolicy controls how a hook fault (error, timeout, or panic) is handled.
// FailOpen contains the fault — log + metric + allow the turn to proceed —
// while FailClosed aborts the turn with a clear reason. Security-gating hooks
// should register FailClosed; everything else defaults to FailOpen at the
// command-hook constructor. Bare NewHookManager/Register keep FailClosed so the
// historical turn-fatal contract for in-process hooks is unchanged.
type FailPolicy int

const (
	// FailClosed aborts the turn when a hook faults (default for Register).
	FailClosed FailPolicy = iota
	// FailOpen contains a hook fault and allows the turn to continue.
	FailOpen
)

func (p FailPolicy) String() string {
	if p == FailOpen {
		return "fail_open"
	}
	return "fail_closed"
}

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
// points, the first non-nil result wins. Each hook carries a FailPolicy that
// governs how a fault (error/timeout/panic) at any point is handled.
type HookManager struct {
	hooks    []Hook
	policies []FailPolicy
}

// NewHookManager builds a manager, skipping nil hooks. Hooks default to
// FailClosed (turn-fatal on fault) — the historical contract.
func NewHookManager(hooks ...Hook) *HookManager {
	return NewHookManagerWithPolicy(FailClosed, hooks...)
}

// NewHookManagerWithPolicy builds a manager whose hooks all share policy.
func NewHookManagerWithPolicy(policy FailPolicy, hooks ...Hook) *HookManager {
	m := &HookManager{}
	for _, h := range hooks {
		m.RegisterWithPolicy(h, policy)
	}
	return m
}

// Register appends h when non-nil with the default FailClosed policy.
func (m *HookManager) Register(h Hook) {
	m.RegisterWithPolicy(h, FailClosed)
}

// RegisterWithPolicy appends h when non-nil with an explicit failure policy.
func (m *HookManager) RegisterWithPolicy(h Hook, policy FailPolicy) {
	if m == nil || h == nil {
		return
	}
	m.hooks = append(m.hooks, h)
	m.policies = append(m.policies, policy)
}

func (m *HookManager) policyAt(i int) FailPolicy {
	if i < 0 || i >= len(m.policies) {
		return FailClosed
	}
	return m.policies[i]
}

// hookFault applies the policy for hook i to a fault observed at point. Under
// FailOpen it records the contained outcome and returns a nil error (allow);
// under FailClosed it records the abort and returns err so the turn dies.
func (m *HookManager) hookFault(i int, point string, err error) error {
	if m.policyAt(i) == FailOpen {
		recordHookOutcome(point, "fail_open")
		slog.Warn("agent hook fault contained", "point", point, "policy", "fail_open", "err", err)
		return nil
	}
	recordHookOutcome(point, "error")
	slog.Warn("agent hook fault aborted turn", "point", point, "policy", "fail_closed", "err", err)
	return err
}

// recoverHook converts a panic in fn into an error so the FailPolicy can decide
// abort-vs-contain, mirroring the db/tx.go recover idiom (D-01).
func recoverHook(point string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			recordRecoveredPanic("hook_" + point)
			err = fmt.Errorf("hook %s panic: %v", point, r)
		}
	}()
	return fn()
}

// OnTurnStart runs registered start hooks in order.
func (m *HookManager) OnTurnStart(ctx context.Context, turn HookTurn) error {
	if m == nil {
		return nil
	}
	for i, h := range m.hooks {
		if err := recoverHook("turn_start", func() error { return h.OnTurnStart(ctx, turn) }); err != nil {
			if aborted := m.hookFault(i, "turn_start", err); aborted != nil {
				return aborted
			}
		}
	}
	recordHookOutcome("turn_start", "ok")
	return nil
}

// BeforeModel gives registered hooks a chance to rewrite or answer a model request.
func (m *HookManager) BeforeModel(ctx context.Context, req *llm.Request) (*ModelHookResult, error) {
	if m == nil {
		return nil, nil
	}
	if req == nil {
		return nil, fmt.Errorf("before_model hook received a nil request")
	}
	for i, h := range m.hooks {
		before := cloneModelRequest(req)
		var res *ModelHookResult
		err := recoverHook("before_model", func() error {
			var e error
			res, e = h.BeforeModel(ctx, req)
			return e
		})
		if err == nil {
			err = validateModelHookMutation(&before, req)
		}
		if err != nil {
			*req = cloneModelRequest(&before)
			if aborted := m.hookFault(i, "before_model", err); aborted != nil {
				return nil, aborted
			}
			continue
		}
		if res != nil {
			recordHookOutcome("before_model", "result")
			return res, nil
		}
	}
	recordHookOutcome("before_model", "allow")
	return nil, nil
}

// BeforeTool gives registered hooks a chance to rewrite or veto a tool call.
func (m *HookManager) BeforeTool(ctx context.Context, call llm.ToolCall) (*ToolHookResult, error) {
	if m == nil {
		return nil, nil
	}
	for i, h := range m.hooks {
		var res *ToolHookResult
		err := recoverHook("before_tool", func() error {
			var e error
			res, e = h.BeforeTool(ctx, call)
			return e
		})
		if err != nil {
			if aborted := m.hookFault(i, "before_tool", err); aborted != nil {
				return nil, aborted
			}
			continue
		}
		if res != nil {
			recordHookOutcome("before_tool", "result")
			return res, nil
		}
	}
	recordHookOutcome("before_tool", "allow")
	return nil, nil
}

// AfterTool gives registered hooks a chance to rewrite a completed tool result.
func (m *HookManager) AfterTool(ctx context.Context, call llm.ToolCall, res tools.ToolResult) (*ToolResultHookResult, error) {
	if m == nil {
		return nil, nil
	}
	for i, h := range m.hooks {
		var next *ToolResultHookResult
		err := recoverHook("after_tool", func() error {
			var e error
			next, e = h.AfterTool(ctx, call, res)
			return e
		})
		if err != nil {
			if aborted := m.hookFault(i, "after_tool", err); aborted != nil {
				return nil, aborted
			}
			continue
		}
		if next != nil {
			recordHookOutcome("after_tool", "result")
			return next, nil
		}
	}
	recordHookOutcome("after_tool", "allow")
	return nil, nil
}

// OnTurnEnd runs registered end hooks in order.
func (m *HookManager) OnTurnEnd(ctx context.Context, turn HookTurn) error {
	if m == nil {
		return nil
	}
	for i, h := range m.hooks {
		if err := recoverHook("turn_end", func() error { return h.OnTurnEnd(ctx, turn) }); err != nil {
			if aborted := m.hookFault(i, "turn_end", err); aborted != nil {
				return aborted
			}
		}
	}
	recordHookOutcome("turn_end", "ok")
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
