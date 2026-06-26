package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// mutatingPanicTool declares Spec.Mutating and panics AFTER simulating a host
// side effect — the exact F-031 shape: a mutating tool that mutates state and
// then crashes. The recovery path must still classify the run as Mutating so the
// completion gate (a.sideEffected) arms.
type mutatingPanicTool struct{}

func (mutatingPanicTool) Spec() tools.Spec {
	return tools.Spec{
		Name:       "fs_write_panic",
		Summary:    "mutating tool that panics after a side effect",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Mutating:   true,
	}
}

func (mutatingPanicTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	panic("mutating tool boom after side effect")
}

// TestRunToolRecovering_MutatingPanicArmsCompletionGate pins F-031: a mutating
// tool that panics after its side effect must yield a recovered toolRunResult
// with Mutating=true. Otherwise dispatch never sets a.sideEffected and the
// completion gate skips its post-mutation safeguard for a turn that DID touch
// host state.
func TestRunToolRecovering_MutatingPanicArmsCompletionGate(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(mutatingPanicTool{})
	a := NewLlmAgent(LlmAgentConfig{
		Client:    &completionLeakClient{},
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:  reg,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
	})
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = "fs_write_panic"
	call.Function.Arguments = `{}`

	run := a.runToolRecovering(context.Background(), budget, call, time.Now().UTC())

	if run.Err == "" {
		t.Fatal("recovered run must carry the panic as an error")
	}
	if !run.Mutating {
		t.Fatal("F-031: a mutating tool that panics must yield Mutating=true so the completion gate arms")
	}
}

// TestRunToolRecovering_NonMutatingPanicStaysNonMutating guards the inverse: a
// non-mutating tool that panics must NOT be reported as mutating, so a read-only
// crash does not spuriously arm the completion gate.
func TestRunToolRecovering_NonMutatingPanicStaysNonMutating(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(nonMutatingPanicTool{})
	a := NewLlmAgent(LlmAgentConfig{
		Client:    &completionLeakClient{},
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:  reg,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
	})
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	call := llm.ToolCall{ID: "c2", Type: "function"}
	call.Function.Name = "ro_panic"
	call.Function.Arguments = `{}`

	run := a.runToolRecovering(context.Background(), budget, call, time.Now().UTC())

	if run.Err == "" {
		t.Fatal("recovered run must carry the panic as an error")
	}
	if run.Mutating {
		t.Fatal("a non-mutating tool that panics must stay Mutating=false")
	}
}

type nonMutatingPanicTool struct{}

func (nonMutatingPanicTool) Spec() tools.Spec {
	return tools.Spec{
		Name:       "ro_panic",
		Summary:    "non-mutating tool that panics",
		Parameters: json.RawMessage(`{"type":"object"}`),
	}
}

func (nonMutatingPanicTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	panic("read-only tool boom")
}

// TestMaxParallelTools_EnvGuard pins maxParallelTools' env parsing: unset/blank,
// non-numeric, zero, and negative all fall back to the safe default, while a valid
// positive value is honored. A bug that returns 0 here would size the executeBatch
// worker pool to zero and deadlock the dispatch, so the <=0 / parse-error guards are
// load-bearing, not cosmetic.
func TestMaxParallelTools_EnvGuard(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want int
	}{
		{"blank_returns_default", "", defaultMaxParallelTools},
		{"whitespace_returns_default", "   ", defaultMaxParallelTools},
		{"valid_positive_is_honored", "8", 8},
		{"one_is_honored", "1", 1},
		{"zero_falls_back", "0", defaultMaxParallelTools},
		{"negative_falls_back", "-3", defaultMaxParallelTools},
		{"non_numeric_falls_back", "abc", defaultMaxParallelTools},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(envMaxParallelTools, c.env)
			if got := maxParallelTools(); got != c.want {
				t.Fatalf("maxParallelTools() with %q = %d, want %d", c.env, got, c.want)
			}
		})
	}
}
