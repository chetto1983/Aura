package agent

// White-box tests for the terminal text_response exclusivity short-circuit in
// dispatch() (LOOP-01 / F-003, D-01/D-01a/D-01b). A terminal text_response is a
// FINAL answer: native tool-use semantics say any tool call present in the step
// means the turn is NOT final, so a text_response alongside ANY runnable sibling
// (mutating OR read-only — classification-independent by design, the Mutating
// flag is untrustworthy) OR a second text_response must reject the whole step
// before any sibling Execute runs. These co-locate here (package agent) so they
// can drive the unexported dispatch() directly and read a.recoveryAttempts /
// a.history — the black-box suite cannot reach either.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// recordingTool flips *ran to true when Execute runs so a test can prove dispatch
// never invoked a sibling beside a terminal text_response. mutating selects the
// Spec hint so the SAME fake covers both the mutating- and read-only-sibling cases.
type recordingTool struct {
	name     string
	mutating bool
	ran      *bool
}

func (rt recordingTool) Spec() tools.Spec {
	return tools.Spec{
		Name:       rt.name,
		Summary:    "records whether Execute ran",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Mutating:   rt.mutating,
	}
}

func (rt recordingTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	*rt.ran = true
	return tools.ToolResult{Preview: "ran", Bytes: 3}, nil
}

// dispatchAgent builds an LlmAgent over a recordingClient + the given registry and
// a fresh root InvocationContext for driving dispatch() directly. The client only
// matters on the finalize reject path (a healthy stream of chunks); the replan and
// control paths never call it.
func dispatchAgent(t *testing.T, reg *tools.Registry, chunks ...llm.Chunk) (*LlmAgent, InvocationContext) {
	t.Helper()
	a := NewLlmAgent(LlmAgentConfig{
		Client:    &recordingClient{chunks: chunks},
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 5},
		Registry:  reg,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
	})
	b, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b}
	return a, ic
}

// terminalCall builds a valid text_response terminal tool call.
func terminalCall(id, text string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"text": text})
	return toolCall(id, terminalTool, string(args))
}

// assertRejectedAll proves EVERY call (with a non-empty ID) received a synthetic
// "rejected:" RoleTool result — the wire stays valid AND no call was silently
// dropped. For the double-terminal case this is the assertion that pins D-01b:
// the second text_response is rejected, not continue-skipped.
func assertRejectedAll(t *testing.T, a *LlmAgent, calls []llm.ToolCall) {
	t.Helper()
	hist := a.HistoryForTest()
	for _, c := range calls {
		found := false
		for _, m := range hist {
			if m.Role == llm.RoleTool && m.ToolCallID == c.ID && strings.HasPrefix(m.Content, "rejected:") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("call %q (%s) has no synthetic reject result — it was silently dropped or executed", c.ID, c.Function.Name)
		}
	}
}

// TestDispatch_TerminalRejectExclusivity pins the D-01 hard-reject: a terminal
// text_response combined with a mutating sibling, a read-only sibling, or a second
// text_response must reject the whole step before any sibling Execute runs and trip
// the replan path (maybeRecover true → done=false), appending a synthetic result for
// every call id.
func TestDispatch_TerminalRejectExclusivity(t *testing.T) {
	cases := []struct {
		name  string
		build func(ran *bool) (*tools.Registry, []llm.ToolCall)
	}{
		{
			name: "terminal_plus_mutating_sibling",
			build: func(ran *bool) (*tools.Registry, []llm.ToolCall) {
				reg := tools.NewRegistry()
				reg.Register(recordingTool{name: "fs_write_fake", mutating: true, ran: ran})
				reg.Register(tools.TextResponse{})
				return reg, []llm.ToolCall{
					toolCall("m1", "fs_write_fake", `{}`),
					terminalCall("t1", "premature final answer"),
				}
			},
		},
		{
			name: "terminal_plus_readonly_sibling",
			build: func(ran *bool) (*tools.Registry, []llm.ToolCall) {
				reg := tools.NewRegistry()
				reg.Register(recordingTool{name: "read_fake", mutating: false, ran: ran})
				reg.Register(tools.TextResponse{})
				return reg, []llm.ToolCall{
					toolCall("r1", "read_fake", `{}`),
					terminalCall("t1", "premature final answer"),
				}
			},
		},
		{
			name: "double_terminal",
			build: func(_ *bool) (*tools.Registry, []llm.ToolCall) {
				reg := tools.NewRegistry()
				reg.Register(tools.TextResponse{})
				return reg, []llm.ToolCall{
					terminalCall("t1", "first answer"),
					terminalCall("t2", "second answer"),
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			reg, calls := tc.build(&ran)
			a, ic := dispatchAgent(t, reg)

			done, infraErr := a.dispatch(ic, [8]byte{}, nil, ic.RequestID.String(), calls, llm.Usage{},
				func(*Event, error) bool { return true })

			if infraErr != nil {
				t.Fatalf("dispatch returned an infra error: %v", infraErr)
			}
			if ran {
				t.Fatal("D-01/F-003: a sibling tool Execute ran despite a terminal text_response in the same step")
			}
			if done {
				t.Fatal("first reject must trip a replan (maybeRecover true → done=false), not finalize")
			}
			if a.recoveryAttempts != 1 {
				t.Fatalf("recoveryAttempts = %d, want 1 (the reject must trip the recover/finalize path)", a.recoveryAttempts)
			}
			assertRejectedAll(t, a, calls)
		})
	}
}

// TestDispatch_TerminalRejectFinalizesAfterRecoveryExhausted proves the SECOND arm
// of the reject short-circuit: when the per-run recovery budget is already spent,
// the reject routes to finalize (done=true) and STILL never runs the sibling.
func TestDispatch_TerminalRejectFinalizesAfterRecoveryExhausted(t *testing.T) {
	ran := false
	reg := tools.NewRegistry()
	reg.Register(recordingTool{name: "fs_write_fake", mutating: true, ran: &ran})
	reg.Register(tools.TextResponse{})
	a, ic := dispatchAgent(t, reg,
		llm.Chunk{Text: "synthesized final answer"},
		llm.Chunk{FinishReason: "stop"},
	)
	a.recoveryAttempts = 1 // recovery already spent → the reject must finalize, not replan

	calls := []llm.ToolCall{
		toolCall("m1", "fs_write_fake", `{}`),
		terminalCall("t1", "premature final answer"),
	}
	events := 0
	done, infraErr := a.dispatch(ic, [8]byte{}, nil, ic.RequestID.String(), calls, llm.Usage{},
		func(*Event, error) bool { events++; return true })

	if infraErr != nil {
		t.Fatalf("dispatch returned an infra error: %v", infraErr)
	}
	if ran {
		t.Fatal("a sibling tool Execute ran on the finalize reject path")
	}
	if !done {
		t.Fatal("with recovery exhausted the reject must finalize (done=true)")
	}
	if events == 0 {
		t.Fatal("finalize must yield a terminal event")
	}
	assertRejectedAll(t, a, calls)
}

// TestDispatch_ReadOnlySiblingWithoutTerminalRuns is the control for the reject:
// a read-only tool with NO terminal in the step executes normally and does not
// finish the loop — the short-circuit must fire only when a terminal is present.
func TestDispatch_ReadOnlySiblingWithoutTerminalRuns(t *testing.T) {
	ran := false
	reg := tools.NewRegistry()
	reg.Register(recordingTool{name: "read_fake", mutating: false, ran: &ran})
	a, ic := dispatchAgent(t, reg)

	calls := []llm.ToolCall{toolCall("r1", "read_fake", `{}`)}
	done, infraErr := a.dispatch(ic, [8]byte{}, nil, ic.RequestID.String(), calls, llm.Usage{},
		func(*Event, error) bool { return true })

	if infraErr != nil {
		t.Fatalf("dispatch returned an infra error: %v", infraErr)
	}
	if done {
		t.Fatal("a non-terminal tool turn must not finish the loop (done=false)")
	}
	if !ran {
		t.Fatal("a read-only tool with no terminal sibling must execute normally")
	}
	if a.recoveryAttempts != 0 {
		t.Fatalf("recoveryAttempts = %d, want 0 (no reject on a plain tool turn)", a.recoveryAttempts)
	}
}

// TestDispatch_SingleTerminalNoSiblingRunsTerminal is the control for the single
// terminal: a lone text_response terminates the loop (done=true) with no reject —
// the short-circuit must not fire when terminalCount==1 and there is no sibling.
func TestDispatch_SingleTerminalNoSiblingRunsTerminal(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	a, ic := dispatchAgent(t, reg)

	calls := []llm.ToolCall{terminalCall("t1", "the final answer")}
	done, infraErr := a.dispatch(ic, [8]byte{}, nil, ic.RequestID.String(), calls, llm.Usage{},
		func(*Event, error) bool { return true })

	if infraErr != nil {
		t.Fatalf("dispatch returned an infra error: %v", infraErr)
	}
	if !done {
		t.Fatal("a single text_response must terminate the loop (done=true)")
	}
	if a.recoveryAttempts != 0 {
		t.Fatalf("recoveryAttempts = %d, want 0 (a lone terminal is not a reject)", a.recoveryAttempts)
	}
	hist := a.HistoryForTest()
	last := hist[len(hist)-1]
	if last.Role != llm.RoleAssistant || last.Content != "the final answer" {
		t.Fatalf("terminal answer = {%v, %q}, want the assistant final answer", last.Role, last.Content)
	}
}
