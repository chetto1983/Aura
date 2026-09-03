package agent_test

// Black-box tests for the completion critic gate (amendment #54 / D-43), driven
// end-to-end through the real Run loop with agenttest.FakeClient. The critic call
// is a real Stream invocation, so it consumes an ordered FakeClient turn — the
// scripts below pin the exact call sequence, which is what makes "a critic call
// happened / did not happen" observable as fc.CallCount().

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// mutatingFakeTool is a non-deferred tool that declares Spec.Mutating so a turn
// dispatching it arms the completion gate, without touching the real filesystem.
type mutatingFakeTool struct{}

func (mutatingFakeTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "fake_write",
		Summary:     "Fake mutating tool.",
		Description: "Fake mutating tool.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}}}`),
		Mutating:    true,
	}
}

func (mutatingFakeTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "wrote:"+string(raw))
}

// newGateAgent builds an agent over a registry with text_response + a read-only
// echo tool + the mutating fake tool, with the completion gate on/off as given.
func newGateAgent(t *testing.T, fc *agenttest.FakeClient, gate bool) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(&echoTool{})
	r.Register(&mutatingFakeTool{})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30, CompletionGate: gate},
		Registry:   r,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "make me a file"}},
	})
}

func mutatingCall(id string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "fake_write", `{"v":"x"}`)
}
func echoCall(id string) llm.ToolCall { return agenttest.MakeToolCall(id, "echo", `{"v":"x"}`) }

// lastFinal returns the terminal Event (the only one carrying a FinishReason).
func lastFinal(evs []*agent.Event) *agent.Event {
	for _, v := range slices.Backward(evs) {
		if v.LLMResponse != nil && v.LLMResponse.FinishReason != "" {
			return v
		}
	}
	return nil
}

func finalContent(t *testing.T, evs []*agent.Event) string {
	t.Helper()
	f := lastFinal(evs)
	if f == nil {
		t.Fatal("no terminal Event with a FinishReason was emitted")
	}
	return f.LLMResponse.Content
}

// TestCompletionGate_Off_AcceptsWithoutCritic: with the gate disabled (the
// zero-value Config default), a side-effecting turn that calls text_response
// terminates immediately and NO critic call is made.
func TestCompletionGate_Off_AcceptsWithoutCritic(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "done, file written")),
	)
	a := newGateAgent(t, fc, false)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2 (no critic call when gate off)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "done, file written" {
		t.Errorf("final = %q, want the accepted answer", got)
	}
}

// TestCompletionGate_ReadOnlyTurn_NowJudged: gate ON, the turn dispatched only a
// read-only tool (no side effect at all) → the gate now REACHES the critic (D-20a
// dropped the side-effect short-circuit entirely), because HARN-06's failure mode is
// exactly a turn that states an intention and dispatches nothing mutating. The
// critic's existing prompt already says a well-supported answer to a question IS
// done, so a DONE verdict here is expected, not a false veto — widening the
// trigger costs one extra critic call, not a wrong outcome.
func TestCompletionGate_ReadOnlyTurn_NowJudged(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(echoCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "here is the answer")),
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newGateAgent(t, fc, true)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 3 {
		t.Fatalf("CallCount = %d, want 3 (a read-only turn now reaches the critic)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "here is the answer" {
		t.Errorf("final = %q, want the accepted answer", got)
	}
	critic := fc.Requests[2]
	if critic.ToolChoice != "none" {
		t.Errorf("critic request ToolChoice = %q, want \"none\" (read-only turn is still critic-judged)", critic.ToolChoice)
	}
}

// TestCompletionGate_Done_AcceptsAfterCritic: gate ON, side effect, critic returns
// DONE → one critic call is made and the termination is accepted. Also pins that
// the critic request is tool-free (ToolChoice="none").
func TestCompletionGate_Done_AcceptsAfterCritic(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "file created and verified")),
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newGateAgent(t, fc, true)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 3 {
		t.Errorf("CallCount = %d, want 3 (1 critic call on a DONE verdict)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "file created and verified" {
		t.Errorf("final = %q, want the accepted answer", got)
	}
	critic := fc.Requests[2]
	if critic.ToolChoice != "none" {
		t.Errorf("critic request ToolChoice = %q, want \"none\"", critic.ToolChoice)
	}
	if critic.Model != "test-model" {
		t.Errorf("critic request Model = %q, want the loop model fallback", critic.Model)
	}
}

// TestCompletionGate_NotDone_VetoesOnceThenAccepts: gate ON, side effect, critic
// returns NOT_DONE once → the first text_response is VETOED (loop continues),
// the critic is consulted again on the second attempt (the veto budget is now 2,
// D-20b) and returns DONE, so the second text_response is accepted. The terminal
// answer is the SECOND text_response, never the first.
func TestCompletionGate_NotDone_VetoesOnceThenAccepts(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "I wrote the script, you run it")),
		agenttest.TextChunks("stop", "NOT_DONE: the xlsx was never produced; run the script and read it back"),
		agenttest.ToolCallTurn(textResponseCall("c3", "file produced and verified at /tmp/out.xlsx")),
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newGateAgent(t, fc, true)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 5 {
		t.Errorf("CallCount = %d, want 5 (mutating + textResp + critic(NOT_DONE) + textResp + critic(DONE))", fc.CallCount())
	}
	got := finalContent(t, evs)
	if got != "file produced and verified at /tmp/out.xlsx" {
		t.Errorf("final = %q, want the SECOND answer (the first was vetoed)", got)
	}
	if got == "I wrote the script, you run it" {
		t.Error("the vetoed hand-off answer was accepted as terminal")
	}
	discards := 0
	for _, ev := range evs {
		if ev != nil && ev.Actions.DiscardStreamed {
			discards++
		}
	}
	if discards != 1 {
		t.Errorf("DiscardStreamed events = %d, want one per veto (amendment #191)", discards)
	}
}

// TestCompletionGate_NotDone_VetoesTwiceThenAccepts: gate ON, a critic that
// answers NOT_DONE on every call it is given — the bounds-exhaustion probe edge
// (D-20b). The first two attempts are vetoed (attempts 1 and 2), the second nudge
// demanding the turn name what did not run. The THIRD attempt is accepted
// UNCONDITIONALLY: completionAttempts >= completionMaxAttempts (2) skips the
// critic entirely, so a third NOT_DONE verdict scripted in the fake client is
// never even consumed — proving the bound is exactly 2, not fail-open masking an
// unbounded loop.
func TestCompletionGate_NotDone_VetoesTwiceThenAccepts(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "attempt 1: I wrote the script, you run it")),
		agenttest.TextChunks("stop", "NOT_DONE: nothing was executed"),
		agenttest.ToolCallTurn(textResponseCall("c3", "attempt 2: still just a plan")),
		agenttest.TextChunks("stop", "NOT_DONE: still nothing executed"),
		agenttest.ToolCallTurn(textResponseCall("c4", "attempt 3: accepted regardless of critic verdict")),
		agenttest.TextChunks("stop", "NOT_DONE: this third verdict must never be consumed"),
	)
	a := newGateAgent(t, fc, true)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 6 {
		t.Fatalf("CallCount = %d, want 6 (mutating+textResp+critic+textResp+critic+textResp; NO third critic call)", fc.CallCount())
	}
	got := finalContent(t, evs)
	if got != "attempt 3: accepted regardless of critic verdict" {
		t.Errorf("final = %q, want the THIRD answer accepted unconditionally", got)
	}
	criticCalls := 0
	for _, req := range fc.Requests {
		if req.ToolChoice == "none" {
			criticCalls++
		}
	}
	if criticCalls != 2 {
		t.Errorf("critic was consulted %d times, want exactly 2 (the bound must not exceed completionMaxAttempts)", criticCalls)
	}
}

// TestCompletionGate_CriticError_FailsOpen: gate ON, side effect, but the critic
// Stream errors → the gate fails OPEN and the original termination is accepted (a
// broken verifier must never wedge a turn).
func TestCompletionGate_CriticError_FailsOpen(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "answer despite broken critic")),
		agenttest.FakeTurn{Err: errors.New("critic transport boom")},
	)
	a := newGateAgent(t, fc, true)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run returned an error; the critic failure must be swallowed (fail-open): %v", err)
	}
	if fc.CallCount() != 3 {
		t.Errorf("CallCount = %d, want 3 (the failed critic call counts)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "answer despite broken critic" {
		t.Errorf("final = %q, want the accepted answer (fail-open)", got)
	}
}

func TestCompletionGate_CriticTransientOpenRetries(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "file created and verified")),
		agenttest.FakeTurn{Err: errors.New("wsarecv: connection reset by peer")},
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newGateAgent(t, fc, true)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 4 {
		t.Fatalf("CallCount = %d, want 4 (critic open retry after transient failure)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "file created and verified" {
		t.Errorf("final = %q, want the accepted answer after critic retry", got)
	}
}

func TestCompletionGate_CriticRetryExhaustedFailsOpen(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "answer after retry exhaustion")),
		agenttest.FakeTurn{Err: errors.New("wsarecv: reset one")},
		agenttest.FakeTurn{Err: errors.New("wsarecv: reset two")},
	)
	a := newGateAgent(t, fc, true)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("critic retry exhaustion must still fail open, got error: %v", err)
	}
	if fc.CallCount() != 4 {
		t.Fatalf("CallCount = %d, want 4 (two critic open attempts)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "answer after retry exhaustion" {
		t.Errorf("final = %q, want fail-open answer", got)
	}
}

// TestCompletionGate_ContentStop_Veto: the content-stop fallback (model emits
// prose, no tool call) is also a voluntary termination. A NOT_DONE veto continues
// the loop one more turn; the critic is consulted again on the second attempt
// (the veto budget is now 2, D-20b) and returns DONE, so the second content-stop
// is accepted.
func TestCompletionGate_ContentStop_Veto(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(mutatingCall("c1")),
		agenttest.TextChunks("stop", "here, run create.py yourself"),
		agenttest.TextChunks("stop", "NOT_DONE: nothing was executed; run it now"),
		agenttest.TextChunks("stop", "produced and verified the output file"),
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newGateAgent(t, fc, true)
	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 5 {
		t.Errorf("CallCount = %d, want 5 (mutating + contentStop + critic(NOT_DONE) + contentStop + critic(DONE))", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "produced and verified the output file" {
		t.Errorf("final = %q, want the second (accepted) content-stop answer", got)
	}
}
