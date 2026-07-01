package agent

// Regression test for LOOP-08 / F-031 (D-13): a mutating tool that panics AFTER
// its side effect must still arm a.sideEffected so the completion gate runs its
// post-mutation safeguard. runToolRecovering already resolves Spec().Mutating
// before Execute and copies it into the panic-recovery result
// (llm_agent_parallel.go); the existing white-box test pins that copy in
// isolation. This test closes the FULL chain — recovery result → dispatch's
// `if run.Mutating { a.sideEffected = true }` → the gate — by driving dispatch()
// end-to-end, which the isolated runToolRecovering test does not exercise.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// sideEffectThenPanicTool models the exact F-031 shape: a Mutating tool that
// performs its host side effect (flips *effected) and THEN crashes. The recovery
// path must classify the run as Mutating even though Execute never returned.
type sideEffectThenPanicTool struct {
	effected *bool
}

func (t sideEffectThenPanicTool) Spec() tools.Spec {
	return tools.Spec{
		Name:       "fs_write_side_effect_panic",
		Summary:    "mutating tool that performs a side effect then panics",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Mutating:   true,
	}
}

func (t sideEffectThenPanicTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	*t.effected = true // the host mutation lands...
	panic("crash AFTER the side effect")
}

// TestDispatch_MutatingPanicArmsCompletionGate pins F-031 through the whole
// dispatch path: a mutating tool that panics after its side effect is recovered
// (no infra error), the loop continues (done=false), and a.sideEffected is armed
// so the completion gate cannot skip its post-mutation check. If
// runToolRecovering stopped copying the Mutating bit, run.Mutating would be false,
// a.sideEffected would stay false, and this test would fail.
func TestDispatch_MutatingPanicArmsCompletionGate(t *testing.T) {
	effected := false
	reg := tools.NewRegistry()
	reg.Register(sideEffectThenPanicTool{effected: &effected})
	a, ic := dispatchAgent(t, reg)

	calls := []llm.ToolCall{toolCall("w1", "fs_write_side_effect_panic", `{}`)}
	done, infraErr := a.dispatch(ic, [8]byte{}, nil, ic.RequestID.String(), calls, llm.Usage{},
		func(*Event, error) bool { return true })

	if infraErr != nil {
		t.Fatalf("dispatch must recover the tool panic, not surface an infra error: %v", infraErr)
	}
	if done {
		t.Fatal("a recovered-panic tool turn is not terminal; the loop must continue (done=false)")
	}
	if !effected {
		t.Fatal("the tool's side effect must have run before the panic")
	}
	if !a.sideEffected {
		t.Fatal("F-031/D-13: a mutating tool that panics after a side effect must arm a.sideEffected (the completion gate)")
	}
}
