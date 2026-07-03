package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
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

// TestCommandHookFailOpen_ContainedAllowsTool is the deliberate CONTRAST to the
// default-deny matrix in hooks_command_hardening_test.go: a command hook
// registered with an EXPLICIT fail_open policy that faults (here it times out)
// has its fault CONTAINED by hookFault — the gate allows and the tool runs. This
// proves the fail_open knob is real. It does NOT prove that a denied command is
// silently allowed: only an explicit opt-in reaches this branch; the default
// (unset) policy denies (see TestCommandHookDefaultPolicyNeverSilentAllows).
func TestCommandHookFailOpen_ContainedAllowsTool(t *testing.T) {
	m := newCommandHookManager(t, "sleep", "fail_open", 1) // explicit opt-in
	executed := false

	err := gateTool(m, agenttest.MakeToolCall("c1", "echo", `{}`), spyTool{executed: &executed})
	if err != nil {
		t.Fatalf("explicit fail_open: gate err = %v, want contained (allow)", err)
	}
	if !executed {
		t.Fatal("explicit fail_open contained the fault but the tool did not run; want allow")
	}
}

// TestCommandHookFailPolicy_BothBranchesDenyVsContain exercises BOTH sides of
// HookManager.hookFault with a real faulting command hook: the default (unset)
// policy DENIES the turn (FailClosed) while an explicit fail_open CONTAINS the
// same fault (FailOpen). Pinning both branches with one fixture guards the
// fail-closed default against a regression that widened containment (F-006).
func TestCommandHookFailPolicy_BothBranchesDenyVsContain(t *testing.T) {
	call := agenttest.MakeToolCall("c1", "echo", `{}`)

	closedExecuted := false
	closed := newCommandHookManager(t, "sleep", "", 1) // default → FailClosed
	if err := gateTool(closed, call, spyTool{executed: &closedExecuted}); err == nil || closedExecuted {
		t.Fatalf("default policy: err=%v executed=%v, want deny + tool-not-run", err, closedExecuted)
	}

	openExecuted := false
	open := newCommandHookManager(t, "sleep", "fail_open", 1) // explicit → FailOpen
	if err := gateTool(open, call, spyTool{executed: &openExecuted}); err != nil || !openExecuted {
		t.Fatalf("fail_open policy: err=%v executed=%v, want contain + tool-run", err, openExecuted)
	}
}

// TestCommandHookDefaultPolicyNeverSilentAllows is the dedicated no-silent-allow
// assertion GATE-02 calls for: across the fault matrix (timeout / crashed-allow /
// unparseable-crash) the DEFAULT policy denies the turn AND the gated tool never
// executes. There is no default-policy code path that lets a denied command run.
func TestCommandHookDefaultPolicyNeverSilentAllows(t *testing.T) {
	faults := []struct {
		name      string
		mode      string
		timeoutMS int
	}{
		{"timeout", "sleep", 1},
		{"crashed_allow", "allow_then_crash", 2000},
		{"unparseable_crash", "crash_no_decision", 2000},
	}
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			m := newCommandHookManager(t, f.mode, "", f.timeoutMS)
			executed := false
			err := gateTool(m, agenttest.MakeToolCall("c1", "echo", `{}`), spyTool{executed: &executed})
			if err == nil {
				t.Fatalf("default policy fault %q: gate err = nil, want DENY", f.mode)
			}
			if executed {
				t.Fatalf("default policy fault %q: tool executed; a denied command was silently allowed", f.mode)
			}
		})
	}
}
