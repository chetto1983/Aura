package agent

// Regression test for LOOP-08 / F-031 (D-13): a mutating tool that panics AFTER
// its side effect must still be classified Mutating, so the post-mutation
// safeguards downstream of that bit still run. runToolRecovering already resolves
// Spec().Mutating before Execute and copies it into the panic-recovery result
// (llm_agent_parallel.go); the existing white-box test pins that copy in
// isolation. This test closes the FULL chain — recovery result → dispatch's
// `if run.Mutating { ... }` → the verify-on-stop gate's evidence — by driving
// dispatch() end-to-end, which the isolated runToolRecovering test does not
// exercise.
//
// The assertion moved from a.sideEffected to a.editedPaths when the dead
// sideEffected flag was removed: the invariant is unchanged, only its surviving
// observable is. Both hang off the same `if run.Mutating` branch, so a regression
// that stops copying the Mutating bit still fails here.

import (
	"context"
	"encoding/json"
	"slices"
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
		// Named for writeToolPathArgs: only a registered write tool leaves a path
		// behind, and the path is what makes the Mutating classification observable.
		Name:       "write_file",
		Summary:    "mutating tool that performs a side effect then panics",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Mutating:   true,
	}
}

func (t sideEffectThenPanicTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	*t.effected = true // the host mutation lands...
	panic("crash AFTER the side effect")
}

// TestDispatch_MutatingPanicRecordsEditedPath pins F-031 through the whole
// dispatch path: a mutating tool that panics after its side effect is recorded
// (no infra error), the loop continues (done=false), and the path it touched is
// recorded as edited so the verify-on-stop gate cannot call the workspace verified
// after it changed. If runToolRecovering stopped copying the Mutating bit,
// run.Mutating would be false, recordEditedPath would never be called, and this
// test would fail.
func TestDispatch_MutatingPanicRecordsEditedPath(t *testing.T) {
	effected := false
	reg := tools.NewRegistry()
	reg.Register(sideEffectThenPanicTool{effected: &effected})
	a, ic := dispatchAgent(t, reg)

	calls := []llm.ToolCall{toolCall("w1", "write_file", `{"path":"/tmp/crashed.txt"}`)}
	done, infraErr := a.dispatch(ic, [8]byte{}, nil, ic.RequestID.String(), calls, &turnUsage{},
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
	if !slices.Contains(a.editedPaths, "/tmp/crashed.txt") {
		t.Fatalf("F-031/D-13: a mutating tool that panics after a side effect must still record its edited path, got %v", a.editedPaths)
	}
}
