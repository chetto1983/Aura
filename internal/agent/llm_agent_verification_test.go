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

func newVerifyGateAgent(
	t *testing.T, fc *agenttest.FakeClient, ledger agent.VerificationLedger,
	completionGate bool, hooks ...agent.Hook,
) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(&echoTool{})
	r.Register(&fakeWriteTool{})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:      fc,
		HookManager: agent.NewHookManager(hooks...),
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

// nudges returns the verify-on-stop messages the agent injected into history, whole:
// the seam decides whether the nudge is a RoleUser message (content-stop, no tool_call
// to answer) or a RoleTool result on the terminal call id (text_response), and both the
// role and the id are part of the contract.
func nudges(t *testing.T, a *agent.LlmAgent) []llm.Message {
	t.Helper()
	var found []llm.Message
	for _, m := range a.HistoryForTest() {
		if strings.Contains(m.Content, "fresh passing verification evidence") {
			found = append(found, m)
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
	if got[0].Role != llm.RoleUser {
		t.Errorf("nudge role = %q, want RoleUser (content-stop has no tool_call to answer)", got[0].Role)
	}
	if !strings.Contains(got[0].Content, file) {
		t.Errorf("nudge does not name the edited path %q: %s", file, got[0].Content)
	}
	if !strings.Contains(got[0].Content, "go test ./...") {
		t.Errorf("nudge does not name the project's verify command: %s", got[0].Content)
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
	assertCriticRanOnlyAfterTheNudgedRound(t, fc)
	if final := finalContent(t, evs); final != "ran go test ./... — green" {
		t.Errorf("final = %q, want the verified answer", final)
	}
}

// assertCriticRanOnlyAfterTheNudgedRound pins the gate ordering on a script of
// write → termination (nudged) → termination (accepted) → critic.
//
// Request[2] is the load-bearing index: it is the round that FOLLOWS the first
// termination candidate, so it is the critic's slot if the paid gate ran first, and an
// ordinary model round if the free gate did. Asserting on request[1] — the termination
// candidate itself — can never fire in either ordering, and reads like a guard while
// being none.
func assertCriticRanOnlyAfterTheNudgedRound(t *testing.T, fc *agenttest.FakeClient) {
	t.Helper()
	if len(fc.Requests) < 4 {
		t.Fatalf("requests = %d, want at least 4 to tell the two orderings apart", len(fc.Requests))
	}
	if fc.Requests[2].ToolChoice == "none" {
		t.Error("the critic ran on the round the verification gate sent back: the free gate must run first")
	}
	if fc.Requests[3].ToolChoice != "none" {
		t.Errorf("request[3].ToolChoice = %q, want the critic call on the accepted round", fc.Requests[3].ToolChoice)
	}
	if fc.CallCount() != 4 {
		t.Errorf("CallCount = %d, want 4 (write + nudged round + accepted round + ONE critic call)", fc.CallCount())
	}
}

// TestVerifyGate_HookDeniedWrite_RecordsNoPath: a BeforeTool policy hook that DENIES a
// write short-circuits into a synthetic result, so the call never reaches Execute and
// never sets Mutating. Nothing was written, so nothing needs verifying — recording the
// path anyway would spend up to two model rounds demanding proof of a write that policy
// blocked.
func TestVerifyGate_HookDeniedWrite_RecordsNoPath(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.ToolCallTurn(textResponseCall("term-1", "the write was blocked by policy")),
	)
	a := newVerifyGateAgent(t, fc, ledger, false, denyingHook{})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2 (a blocked write leaves nothing to verify)", fc.CallCount())
	}
	if n := nudges(t, a); len(n) != 0 {
		t.Errorf("nudges = %d, want 0: the write never happened", len(n))
	}
	if ledger.reads != 0 {
		t.Errorf("ledger status reads = %d, want 0 (no edited path to ask about)", ledger.reads)
	}
	if got := finalContent(t, evs); got != "the write was blocked by policy" {
		t.Errorf("final = %q, want the first answer", got)
	}
}

// denyingHook vetoes write_file the way a policy command hook's "deny" decision does
// (hooks_command.go): BeforeTool returns a synthetic ToolResult, dispatch skips
// execution entirely, and the run carries no Mutating flag.
type denyingHook struct{}

func (denyingHook) OnTurnStart(context.Context, agent.HookTurn) error { return nil }

func (denyingHook) BeforeModel(context.Context, *llm.Request) (*agent.ModelHookResult, error) {
	return nil, nil
}

func (denyingHook) BeforeTool(_ context.Context, call llm.ToolCall) (*agent.ToolHookResult, error) {
	if call.Function.Name != "write_file" {
		return nil, nil
	}
	return &agent.ToolHookResult{Result: &tools.ToolResult{Preview: "denied: policy blocked this write"}}, nil
}

func (denyingHook) AfterTool(context.Context, llm.ToolCall, tools.ToolResult) (*agent.ToolResultHookResult, error) {
	return nil, nil
}

func (denyingHook) OnTurnEnd(context.Context, agent.HookTurn) error { return nil }

// TestVerifyGate_TextResponse_NudgesAsAToolResult: text_response is the terminal a
// tool-calling model actually reaches, and it has a tool_call awaiting an answer — so
// the nudge must ride a RoleTool result carrying that call id, not the RoleUser message
// the content-stop seam uses. A RoleUser message here would leave the text_response
// unanswered and break the wire.
func TestVerifyGate_TextResponse_NudgesAsAToolResult(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified", "passed"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.ToolCallTurn(textResponseCall("term-1", "patched it, should be fine")),
		agenttest.ToolCallTurn(textResponseCall("term-2", "ran go test ./... — green")),
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
		t.Fatalf("nudges = %d, want exactly 1", len(got))
	}
	if got[0].Role != llm.RoleTool {
		t.Errorf("nudge role = %q, want RoleTool (the terminal call must be answered)", got[0].Role)
	}
	if got[0].ToolCallID != "term-1" {
		t.Errorf("nudge ToolCallID = %q, want %q (the terminal call it answers)", got[0].ToolCallID, "term-1")
	}
	if !strings.Contains(got[0].Content, file) {
		t.Errorf("nudge does not name the edited path %q: %s", file, got[0].Content)
	}
	if final := finalContent(t, evs); final != "ran go test ./... — green" {
		t.Errorf("final = %q, want the answer after the nudge", final)
	}
}

// TestVerifyGate_AttemptsAreSharedAcrossSeams: the counter lives on the agent, so the
// budget is two nudges per RUN — not two per seam. A turn that terminates through the
// content-stop fallback and then through text_response spends both, and the third
// termination is accepted whichever seam it arrives on.
func TestVerifyGate_AttemptsAreSharedAcrossSeams(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.TextChunks("stop", "first answer"),
		agenttest.ToolCallTurn(textResponseCall("term-1", "second answer")),
		agenttest.ToolCallTurn(textResponseCall("term-2", "third answer")),
	)
	a := newVerifyAgent(t, fc, ledger)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 4 {
		t.Fatalf("CallCount = %d, want 4 (write + one nudge per seam + the accepted answer)", fc.CallCount())
	}
	got := nudges(t, a)
	if len(got) != 2 {
		t.Fatalf("nudges = %d, want exactly 2 across BOTH seams (verificationMaxAttempts is per run)", len(got))
	}
	if got[0].Role != llm.RoleUser || got[1].Role != llm.RoleTool {
		t.Errorf("nudge roles = (%q, %q), want (RoleUser from content-stop, RoleTool from text_response)",
			got[0].Role, got[1].Role)
	}
	if final := finalContent(t, evs); final != "third answer" {
		t.Errorf("final = %q, want the third answer (the shared counter is spent)", final)
	}
}

// TestVerifyGate_TextResponse_RunsBeforeTheCompletionCritic mirrors the content-stop
// ordering case on the terminal that actually fires in production: a round the free gate
// sends back must never have paid for the critic.
func TestVerifyGate_TextResponse_RunsBeforeTheCompletionCritic(t *testing.T) {
	dir, file := projectFile(t)
	ledger := &stubLedger{dir: dir, statuses: []string{"unverified", "passed"}}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(writeCall("c1", file)),
		agenttest.ToolCallTurn(textResponseCall("term-1", "patched it, should be fine")),
		agenttest.ToolCallTurn(textResponseCall("term-2", "ran go test ./... — green")),
		agenttest.TextChunks("stop", "DONE"),
	)
	a := newVerifyGateAgent(t, fc, ledger, true)

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCriticRanOnlyAfterTheNudgedRound(t, fc)
	if final := finalContent(t, evs); final != "ran go test ./... — green" {
		t.Errorf("final = %q, want the verified answer", final)
	}
}

// TestVerifyGate_TextResponse_DoesNotNudge covers, on the text_response terminal, the
// four ways the gate has nothing to say — the same cases the content-stop seam pins one
// by one above.
func TestVerifyGate_TextResponse_DoesNotNudge(t *testing.T) {
	dir, file := projectFile(t)
	cases := []struct {
		name      string
		ledger    *stubLedger // nil → no ledger wired at all
		firstCall llm.ToolCall
		env       string
		wantReads int
	}{
		{"nil_ledger", nil, writeCall("c1", file), "", 0},
		{"no_edited_paths", &stubLedger{dir: dir, statuses: []string{"unverified"}}, echoCall("c1"), "", 0},
		{"env_off", &stubLedger{dir: dir, statuses: []string{"unverified"}}, writeCall("c1", file), "0", 0},
		{"passed_workspace", &stubLedger{dir: dir, statuses: []string{"passed"}}, writeCall("c1", file), "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("AURA_AGENT_VERIFY_ON_STOP_ENABLED", tc.env)
			}
			// A typed nil would satisfy the interface and defeat the nil-ledger case.
			var ledger agent.VerificationLedger
			if tc.ledger != nil {
				ledger = tc.ledger
			}
			fc := agenttest.NewFakeClient(
				agenttest.ToolCallTurn(tc.firstCall),
				agenttest.ToolCallTurn(textResponseCall("term-1", "patched it")),
			)
			a := newVerifyAgent(t, fc, ledger)

			evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{})))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if fc.CallCount() != 2 {
				t.Errorf("CallCount = %d, want 2 (no nudge)", fc.CallCount())
			}
			if n := nudges(t, a); len(n) != 0 {
				t.Errorf("nudges = %d, want 0", len(n))
			}
			if tc.ledger != nil && tc.ledger.reads != tc.wantReads {
				t.Errorf("ledger status reads = %d, want %d", tc.ledger.reads, tc.wantReads)
			}
			if got := finalContent(t, evs); got != "patched it" {
				t.Errorf("final = %q, want the first answer", got)
			}
		})
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
