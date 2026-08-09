package agent_test

// Black-box tests for the verify-on-stop gate, driven end-to-end through the real Run
// loop with agenttest.FakeClient. The nudge is injected as a user-role message and the
// loop re-enters the model round, so "it nudged" is observable twice over: as an extra
// scripted turn consumed (fc.CallCount) and as a RoleUser message in history.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// fakeWriteTool is named write_file so the loop's edited-path accumulator recognises
// it (writeToolPathArgs), without touching the real filesystem.
type fakeWriteTool struct{}

func (fakeWriteTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "write_file",
		Summary:     "Fake write tool.",
		Description: "Fake write tool.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Mutating:    true,
	}
}

func (fakeWriteTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "wrote:"+string(raw))
}

// stubLedger recognises exactly one directory as a project and answers the statuses it
// was scripted with, one per read, repeating the last — so a test can model a workspace
// that stays unverified as easily as one the agent then verified.
type stubLedger struct {
	dir      string
	statuses []string
	reads    int
}

func (l *stubLedger) ProjectFactsFor(cwd string) agent.ProjectFacts {
	if cwd != l.dir {
		return agent.ProjectFacts{}
	}
	return agent.ProjectFacts{Found: true, Root: l.dir, VerifyCommands: []string{"go test ./..."}}
}

func (l *stubLedger) VerificationStatusFor(_, cwd string) agent.VerificationStatus {
	if cwd != l.dir {
		return agent.VerificationStatus{Status: "not_applicable"}
	}
	status := l.statuses[min(l.reads, len(l.statuses)-1)]
	l.reads++
	return agent.VerificationStatus{Status: status}
}

// newVerifyAgent builds an agent over text_response + echo + the fake write tool, with
// the given ledger (nil = gate disabled) and the completion critic off.
func newVerifyAgent(t *testing.T, fc *agenttest.FakeClient, ledger agent.VerificationLedger) *agent.LlmAgent {
	t.Helper()
	return newVerifyGateAgent(t, fc, ledger, false)
}

func newVerifyGateAgent(t *testing.T, fc *agenttest.FakeClient, ledger agent.VerificationLedger, completionGate bool) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(&echoTool{})
	r.Register(&fakeWriteTool{})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client: fc,
		LLM: llm.Config{
			Model: "test-model", Provider: "test-provider",
			TotalTimeoutSec: 30, CompletionGate: completionGate,
		},
		Registry:   r,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "fix the parser"}},
		Ledger:     ledger,
	})
}

// projectFile returns a project directory and a code file inside it, so
// filepath.Dir(file) is exactly the directory the ledger answers for on any OS.
func projectFile(t *testing.T) (dir, file string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "proj")
	return dir, filepath.Join(dir, "app.go")
}

func writeCall(id, path string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return agenttest.MakeToolCall(id, "write_file", string(args))
}

// nudges returns the verify-on-stop messages the agent injected into history.
func nudges(t *testing.T, a *agent.LlmAgent) []string {
	t.Helper()
	var found []string
	for _, m := range a.HistoryForTest() {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "fresh passing verification evidence") {
			found = append(found, m.Content)
		}
	}
	return found
}

// TestVerifyGate_NilLedger_NeverNudges: with no ledger wired (no pool, tests,
// standalone) the gate is inert — an edited turn terminates on its first answer.
func TestVerifyGate_NilLedger_NeverNudges(t *testing.T) {
	_, file := projectFile(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "patched the parser"),
	)
	a := newVerifyAgent(t, fc, nil)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2 (a nil ledger must not nudge)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "patched the parser" {
		t.Errorf("final = %q, want the first answer", got)
	}
	if n := nudges(t, a); len(n) != 0 {
		t.Errorf("nudges = %d, want 0", len(n))
	}
}

// TestVerifyGate_NoEditedPaths_NeverNudges: a read-only turn edited nothing, so there
// is nothing to verify — the ledger is never even consulted.
func TestVerifyGate_NoEditedPaths_NeverNudges(t *testing.T) {
	dir, _ := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(echoCall("c1")),
		agenttest.TextChunks("stop", "here is what the parser does"),
	)
	a := newVerifyAgent(t, fc, ledger)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2 (a read-only turn must not nudge)", fc.CallCount())
	}
	if got := finalContent(t, evs); got != "here is what the parser does" {
		t.Errorf("final = %q, want the first answer", got)
	}
	if ledger.reads != 0 {
		t.Errorf("ledger status reads = %d, want 0 (no edited path to ask about)", ledger.reads)
	}
}

// TestVerifyGate_UnverifiedEdit_NudgesOnceAndContinues: an edit in a project with no
// fresh evidence is sent back once — the nudge lands in history as a RoleUser message
// naming the project's own verify command, the loop re-enters the model round, and the
// answer that terminates the turn is the one AFTER the verification passed.
func TestVerifyGate_UnverifiedEdit_NudgesOnceAndContinues(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified", "passed"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "patched it, should be fine"),
		agenttest.TextChunks("stop", "ran go test ./... — green"),
	)
	a := newVerifyAgent(t, fc, ledger)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 3 {
		t.Fatalf("CallCount = %d, want 3 (the nudge must re-enter the model round)", fc.CallCount())
	}
	got := nudges(t, a)
	if len(got) != 1 {
		t.Fatalf("nudges = %d, want exactly 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], file) {
		t.Errorf("nudge does not name the edited path %q: %s", file, got[0])
	}
	if !strings.Contains(got[0], "go test ./...") {
		t.Errorf("nudge does not name the project's verify command: %s", got[0])
	}
	if final := finalContent(t, evs); final != "ran go test ./... — green" {
		t.Errorf("final = %q, want the answer after the nudge", final)
	}
}

// TestVerifyGate_SecondAttemptRefused: a workspace that never reports passing gets
// verificationMaxAttempts (2) nudges and no more — the gate is bounded, so a ledger
// stuck on "unverified" can never wedge the turn.
func TestVerifyGate_SecondAttemptRefused(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "first answer"),
		agenttest.TextChunks("stop", "second answer"),
		agenttest.TextChunks("stop", "third answer"),
	)
	a := newVerifyAgent(t, fc, ledger)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 4 {
		t.Fatalf("CallCount = %d, want 4 (write + 2 nudged rounds + the accepted answer)", fc.CallCount())
	}
	if got := nudges(t, a); len(got) != 2 {
		t.Fatalf("nudges = %d, want exactly 2 (verificationMaxAttempts)", len(got))
	}
	if final := finalContent(t, evs); final != "third answer" {
		t.Errorf("final = %q, want the third answer (the gate stops nudging)", final)
	}
}

// TestVerifyGate_EnvDisablesEntirely: AURA_AGENT_VERIFY_ON_STOP_ENABLED=0 is the
// operator off-switch — the same unverified edit terminates on its first answer.
func TestVerifyGate_EnvDisablesEntirely(t *testing.T) {
	t.Setenv("AURA_AGENT_VERIFY_ON_STOP_ENABLED", "0")
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "patched it, should be fine"),
	)
	a := newVerifyAgent(t, fc, ledger)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2 (the env off-switch disables the gate)", fc.CallCount())
	}
	if ledger.reads != 0 {
		t.Errorf("ledger status reads = %d, want 0 (a disabled gate must not consult the ledger)", ledger.reads)
	}
	if got := finalContent(t, evs); got != "patched it, should be fine" {
		t.Errorf("final = %q, want the first answer", got)
	}
}

// TestVerifyGate_RunsBeforeTheCompletionCritic: the verification gate is deterministic
// and free, the completion critic costs a model call, so a round the free gate sends
// back must never have paid for the critic. With both gates armed the critic call
// appears only in the round the verification gate accepted — index 3, after the nudged
// round, not before it.
func TestVerifyGate_RunsBeforeTheCompletionCritic(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified", "passed"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "patched it, should be fine"),
		agenttest.TextChunks("stop", "ran go test ./... — green"),
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newVerifyGateAgent(t, fc, ledger, true)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 4 {
		t.Fatalf("CallCount = %d, want 4 (write + nudged round + accepted round + ONE critic call)", fc.CallCount())
	}
	if fc.Requests[1].ToolChoice == "none" {
		t.Error("the critic ran on the round the verification gate sent back: the free gate must run first")
	}
	if fc.Requests[3].ToolChoice != "none" {
		t.Errorf("request[3].ToolChoice = %q, want the critic call on the accepted round", fc.Requests[3].ToolChoice)
	}
	if final := finalContent(t, evs); final != "ran go test ./... — green" {
		t.Errorf("final = %q, want the verified answer", final)
	}
}

// TestVerifyGate_PassedWorkspace_NeverNudges: the ledger already holds fresh passing
// evidence for the edited workspace, which is exactly what the gate asks for.
func TestVerifyGate_PassedWorkspace_NeverNudges(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"passed"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "patched it and the suite is green"),
	)
	a := newVerifyAgent(t, fc, ledger)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2 (a passing workspace satisfies the gate)", fc.CallCount())
	}
	if ledger.reads != 1 {
		t.Errorf("ledger status reads = %d, want 1 (the gate asks once, then accepts)", ledger.reads)
	}
	if got := finalContent(t, evs); got != "patched it and the suite is green" {
		t.Errorf("final = %q, want the first answer", got)
	}
}
