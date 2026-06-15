package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// faultyHook returns an error, panics, or both from a chosen hook point.
type faultyHook struct {
	failPoint string
	panicAt   string
}

func (h faultyHook) maybe(point string) error {
	if h.panicAt == point {
		panic("hook boom at " + point)
	}
	if h.failPoint == point {
		return errors.New("hook failure at " + point)
	}
	return nil
}

func (h faultyHook) OnTurnStart(_ context.Context, _ agent.HookTurn) error {
	return h.maybe("turn_start")
}

func (h faultyHook) BeforeModel(_ context.Context, _ *llm.Request) (*agent.ModelHookResult, error) {
	return nil, h.maybe("before_model")
}

func (h faultyHook) BeforeTool(_ context.Context, _ llm.ToolCall) (*agent.ToolHookResult, error) {
	return nil, h.maybe("before_tool")
}

func (h faultyHook) AfterTool(_ context.Context, _ llm.ToolCall, _ tools.ToolResult) (*agent.ToolResultHookResult, error) {
	return nil, h.maybe("after_tool")
}

func (h faultyHook) OnTurnEnd(_ context.Context, _ agent.HookTurn) error {
	return h.maybe("turn_end")
}

func TestHookFailOpen_ErrorIsContained(t *testing.T) {
	m := agent.NewHookManagerWithPolicy(agent.FailOpen, faultyHook{failPoint: "before_model"})

	res, err := m.BeforeModel(context.Background(), &llm.Request{})
	if err != nil {
		t.Fatalf("BeforeModel under fail_open returned err = %v, want contained", err)
	}
	if res != nil {
		t.Fatalf("BeforeModel under fail_open res = %+v, want allow (nil)", res)
	}
}

func TestHookFailOpen_PanicIsContained(t *testing.T) {
	m := agent.NewHookManagerWithPolicy(agent.FailOpen, faultyHook{panicAt: "before_tool"})

	res, err := m.BeforeTool(context.Background(), llm.ToolCall{})
	if err != nil {
		t.Fatalf("BeforeTool under fail_open returned err = %v, want contained", err)
	}
	if res != nil {
		t.Fatalf("BeforeTool under fail_open res = %+v, want allow (nil)", res)
	}
}

func TestHookFailOpen_TurnLifecycleContained(t *testing.T) {
	m := agent.NewHookManagerWithPolicy(agent.FailOpen, faultyHook{failPoint: "turn_start", panicAt: "turn_end"})

	if err := m.OnTurnStart(context.Background(), agent.HookTurn{}); err != nil {
		t.Fatalf("OnTurnStart under fail_open err = %v, want contained", err)
	}
	if err := m.OnTurnEnd(context.Background(), agent.HookTurn{}); err != nil {
		t.Fatalf("OnTurnEnd under fail_open err = %v, want contained", err)
	}
}

func TestHookFailClosed_ErrorAborts(t *testing.T) {
	m := agent.NewHookManagerWithPolicy(agent.FailClosed, faultyHook{failPoint: "before_model"})

	_, err := m.BeforeModel(context.Background(), &llm.Request{})
	if err == nil {
		t.Fatal("BeforeModel under fail_closed err = nil, want abort")
	}
	if !strings.Contains(err.Error(), "hook failure") {
		t.Fatalf("BeforeModel under fail_closed err = %v, want clear reason", err)
	}
}

func TestHookFailClosed_PanicAbortsWithReason(t *testing.T) {
	m := agent.NewHookManagerWithPolicy(agent.FailClosed, faultyHook{panicAt: "before_tool"})

	_, err := m.BeforeTool(context.Background(), llm.ToolCall{})
	if err == nil {
		t.Fatal("BeforeTool under fail_closed err = nil, want abort on panic")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("BeforeTool under fail_closed err = %v, want panic reason", err)
	}
}

// TestHookDefaultPolicyStaysFailClosed preserves the existing turn-fatal default
// for bare NewHookManager registration so TestHookMetric_ErrorOutcome holds.
func TestHookDefaultPolicyStaysFailClosed(t *testing.T) {
	m := agent.NewHookManager(faultyHook{failPoint: "before_model"})

	_, err := m.BeforeModel(context.Background(), &llm.Request{})
	if err == nil {
		t.Fatal("BeforeModel with default policy err = nil, want fail_closed default")
	}
}
