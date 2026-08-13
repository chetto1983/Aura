package agent

import (
	"context"
	"strconv"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// The hook is the WRITE half of the verification ledger: everything the gate later
// reads exists because AfterTool decided a finished call was worth recording. These
// pin those decisions -- which finished command is evidence, which finished call
// stales it, and above all what is recorded for neither -- without a Postgres pool,
// through the consumer-declared verificationRecorder seam.

type recordedTerminal struct {
	identityID string
	input      ClassifyVerificationCommandInput
}

type recordedEdit struct {
	identityID, sessionID, root string
	paths                       []string
}

type fakeRecorder struct {
	terminals []recordedTerminal
	edits     []recordedEdit
}

func (f *fakeRecorder) RecordTerminalResult(
	_ context.Context,
	identityID string,
	input ClassifyVerificationCommandInput,
) (VerificationCommandEvidence, bool, error) {
	f.terminals = append(f.terminals, recordedTerminal{identityID: identityID, input: input})
	return VerificationCommandEvidence{}, true, nil
}

func (f *fakeRecorder) MarkWorkspaceEdited(
	_ context.Context,
	identityID, sessionID, root string,
	paths []string,
) error {
	f.edits = append(f.edits, recordedEdit{identityID: identityID, sessionID: sessionID, root: root, paths: paths})
	return nil
}

// stubDetector recognises exactly the directories it was given and reports every other
// one as "not a project" -- which is all the ledger's two halves ever ask a detector.
//
// It replaces a real-directory fixture because the production detector no longer stats
// anything reachable from a test: it probes the per-identity sandbox box
// (coding_context_box.go), whose /workspace is a different volume from this process's. A
// fixture built with os.MkdirTemp would therefore be testing a filesystem the shipped
// code never reads.
type stubDetector map[string]ProjectFacts

func (d stubDetector) ProjectFactsFor(cwd string) ProjectFacts { return d[cwd] }

// hookProjectRoot is a BOX path: the hook resolves a written file to its directory with
// path.Dir, so these fixtures are POSIX on every host.
const hookProjectRoot = "/workspace/fixture"

func newHook(t *testing.T) (*VerificationHook, *fakeRecorder) {
	t.Helper()
	rec := &fakeRecorder{}
	return &VerificationHook{
		Store:      rec,
		Detector:   stubDetector{hookProjectRoot: {Found: true, Root: hookProjectRoot}},
		IdentityID: "identity-1",
		SessionID:  "session-1",
	}, rec
}

// hookCall builds one dispatched call. llm.ToolCall's Function is an anonymous
// struct, so it cannot be set in a composite literal -- the whole package builds
// them this way.
func hookCall(name, arguments string) llm.ToolCall {
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}

func shellCall(command string, background bool) llm.ToolCall {
	args := `{"command":` + strconv.Quote(command) + `}`
	if background {
		args = `{"command":` + strconv.Quote(command) + `,"background":true}`
	}
	return hookCall("shell_exec", args)
}

func writeCall(name, path string) llm.ToolCall {
	return hookCall(name, `{"path":`+strconv.Quote(path)+`,"content":"package main"}`)
}

func shellResult(meta tools.ToolResultMeta) tools.ToolResult {
	return tools.ToolResult{Preview: "ok\n", Meta: &meta}
}

func afterTool(t *testing.T, h *VerificationHook, call llm.ToolCall, result tools.ToolResult) {
	t.Helper()
	next, err := h.AfterTool(context.Background(), call, result)
	if err != nil {
		t.Fatalf("AfterTool must never error: %v", err)
	}
	// The ledger observes; it never rewrites what the model sees.
	if next != nil {
		t.Fatalf("AfterTool must not rewrite the tool result, got %+v", next)
	}
}

func TestVerificationHookRecordsAFinishedShellCommand(t *testing.T) {
	root := hookProjectRoot
	h, rec := newHook(t)

	afterTool(t, h, shellCall("go test ./...", false),
		shellResult(tools.ToolResultMeta{"cwd": root, "exit_code": 0}))

	if len(rec.terminals) != 1 {
		t.Fatalf("recorded %d terminal results, want 1", len(rec.terminals))
	}
	got := rec.terminals[0]
	if got.identityID != "identity-1" {
		t.Errorf("identity = %q, want identity-1", got.identityID)
	}
	if got.input.SessionID != "session-1" {
		t.Errorf("session = %q, want session-1", got.input.SessionID)
	}
	if got.input.Command != "go test ./..." {
		t.Errorf("command = %q", got.input.Command)
	}
	if got.input.CWD != root || got.input.Root != root {
		t.Errorf("cwd/root = %q/%q, want %q for both", got.input.CWD, got.input.Root, root)
	}
	if got.input.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", got.input.ExitCode)
	}
	if got.input.Output != "ok\n" {
		t.Errorf("output = %q, want the result preview", got.input.Output)
	}
	if len(rec.edits) != 0 {
		t.Errorf("a shell command must not mark the workspace edited: %+v", rec.edits)
	}
}

func TestVerificationHookRecordsAFailingCommandToo(t *testing.T) {
	// The ledger records what was PROVED, not only what succeeded: a failing suite is
	// evidence, and dropping it would let a red workspace read as never-verified.
	root := hookProjectRoot
	h, rec := newHook(t)

	afterTool(t, h, shellCall("go test ./...", false),
		shellResult(tools.ToolResultMeta{"cwd": root, "exit_code": 1}))

	if len(rec.terminals) != 1 {
		t.Fatalf("recorded %d terminal results, want 1", len(rec.terminals))
	}
	if rec.terminals[0].input.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", rec.terminals[0].input.ExitCode)
	}
}

func TestVerificationHookRecordsNothingForShellCallsThatProvedNothing(t *testing.T) {
	root := hookProjectRoot

	cases := map[string]struct {
		call   llm.ToolCall
		result tools.ToolResult
	}{
		// Still running: it has not finished, so it has proved nothing yet.
		"backgrounded": {shellCall("go test ./...", true), shellResult(tools.ToolResultMeta{"cwd": root, "exit_code": 0})},
		// No exit code means the command never ran to completion (approval gate,
		// timeout kill) -- an outcome, not a result.
		"no exit code": {shellCall("go test ./...", false), shellResult(tools.ToolResultMeta{"cwd": root, "timed_out": true})},
		"no meta":      {shellCall("go test ./...", false), tools.ToolResult{Preview: "ok"}},
		"no cwd":       {shellCall("go test ./...", false), shellResult(tools.ToolResultMeta{"exit_code": 0})},
		// A directory the detector does not recognise is not a workspace to prove.
		"outside a project": {shellCall("go test ./...", false), shellResult(tools.ToolResultMeta{"cwd": "/workspace/elsewhere", "exit_code": 0})},
		"unparseable args": {
			hookCall("shell_exec", "{"),
			shellResult(tools.ToolResultMeta{"cwd": root, "exit_code": 0}),
		},
		"empty command": {
			hookCall("shell_exec", `{"command":"  "}`),
			shellResult(tools.ToolResultMeta{"cwd": root, "exit_code": 0}),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, rec := newHook(t)
			afterTool(t, h, tc.call, tc.result)
			if len(rec.terminals) != 0 {
				t.Fatalf("recorded %+v, want nothing", rec.terminals)
			}
		})
	}
}

func TestVerificationHookMarksAWriteToolsWorkspaceEdited(t *testing.T) {
	root := hookProjectRoot
	edited := root + "/app.go"
	h, rec := newHook(t)

	afterTool(t, h, writeCall("write_file", edited), tools.ToolResult{Preview: "wrote"})

	if len(rec.edits) != 1 {
		t.Fatalf("recorded %d edits, want 1", len(rec.edits))
	}
	got := rec.edits[0]
	if got.identityID != "identity-1" || got.sessionID != "session-1" {
		t.Errorf("edit scoped to %q/%q, want identity-1/session-1", got.identityID, got.sessionID)
	}
	if got.root != root {
		t.Errorf("root = %q, want %q -- the ledger keys on the project root, not the file", got.root, root)
	}
	if len(got.paths) != 1 || got.paths[0] != edited {
		t.Errorf("paths = %v, want [%s]", got.paths, edited)
	}
	if len(rec.terminals) != 0 {
		t.Errorf("a write must not record verification evidence: %+v", rec.terminals)
	}
}

func TestVerificationHookIgnoresCallsItHasNothingToSayAbout(t *testing.T) {
	root := hookProjectRoot

	cases := map[string]llm.ToolCall{
		// Not a write tool: writeToolPathArgs is the whole list, and a tool absent
		// from it leaves no path to stale.
		"unknown tool":            hookCall("web_search", `{"query":"go"}`),
		"read tool":               writeCall("read_file", root+"/app.go"),
		"write with no path":      hookCall("write_file", `{"content":"x"}`),
		"write outside a project": writeCall("write_file", "/workspace/elsewhere/app.go"),
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			h, rec := newHook(t)
			afterTool(t, h, call, tools.ToolResult{Preview: "done"})
			if len(rec.terminals) != 0 || len(rec.edits) != 0 {
				t.Fatalf("recorded %+v / %+v, want nothing", rec.terminals, rec.edits)
			}
		})
	}
}

func TestVerificationHookWithoutAHalfIsInert(t *testing.T) {
	// The two disabled deployments, each of which must degrade to doing nothing rather
	// than to a nil-dereference on the turn's own goroutine: no pool behind
	// NewEvidenceStore, and no detector (a composition root with no box router to give
	// one). Both are nil-interface fields, so both would panic without the guard.
	hooks := map[string]*VerificationHook{
		"no store":    {Detector: stubDetector{hookProjectRoot: {Found: true, Root: hookProjectRoot}}},
		"no detector": {Store: &fakeRecorder{}},
	}
	for name, h := range hooks {
		t.Run(name, func(t *testing.T) {
			h.IdentityID, h.SessionID = "identity-1", "session-1"
			afterTool(t, h, shellCall("go test ./...", false),
				shellResult(tools.ToolResultMeta{"cwd": hookProjectRoot, "exit_code": 0}))
			afterTool(t, h, writeCall("write_file", hookProjectRoot+"/app.go"),
				tools.ToolResult{Preview: "wrote"})
		})
	}
}

func TestVerificationHookObservesOnly(t *testing.T) {
	// The other four Hook points exist to satisfy the interface and must stay silent:
	// a hook that steers a request or vetoes a call is not an observer.
	h, rec := newHook(t)
	ctx := context.Background()

	if err := h.OnTurnStart(ctx, HookTurn{}); err != nil {
		t.Fatalf("OnTurnStart: %v", err)
	}
	req := &llm.Request{Model: "m"}
	res, err := h.BeforeModel(ctx, req)
	if res != nil || err != nil {
		t.Fatalf("BeforeModel = %+v, %v; want nil, nil", res, err)
	}
	tres, err := h.BeforeTool(ctx, shellCall("rm -rf /", false))
	if tres != nil || err != nil {
		t.Fatalf("BeforeTool = %+v, %v; want nil, nil", tres, err)
	}
	if err := h.OnTurnEnd(ctx, HookTurn{}); err != nil {
		t.Fatalf("OnTurnEnd: %v", err)
	}
	if len(rec.terminals) != 0 || len(rec.edits) != 0 {
		t.Fatalf("no hook point but AfterTool may write to the ledger: %+v / %+v", rec.terminals, rec.edits)
	}
}

// panickingRecorder is a ledger that is down hard -- the worst case the FailOpen
// registration exists for.
type panickingRecorder struct{}

func (panickingRecorder) RecordTerminalResult(
	context.Context, string, ClassifyVerificationCommandInput,
) (VerificationCommandEvidence, bool, error) {
	panic("verification ledger unreachable")
}

func (panickingRecorder) MarkWorkspaceEdited(context.Context, string, string, string, []string) error {
	panic("verification ledger unreachable")
}

func TestVerificationHookMustBeRegisteredFailOpen(t *testing.T) {
	// The runner registers this hook with FailOpen (runner_verification.go), and this
	// is why: the ledger is an OBSERVER, so a ledger outage that aborted the turn it
	// was watching would make recording evidence more dangerous than not recording it.
	// Register's FailClosed default does exactly that, so the choice is load-bearing.
	root := hookProjectRoot
	h := &VerificationHook{
		Store:      panickingRecorder{},
		Detector:   stubDetector{root: {Found: true, Root: root}},
		IdentityID: "identity-1",
		SessionID:  "session-1",
	}
	call := shellCall("go test ./...", false)
	result := shellResult(tools.ToolResultMeta{"cwd": root, "exit_code": 0})

	open := NewHookManager().With(h, FailOpen)
	if _, err := open.AfterTool(context.Background(), call, result); err != nil {
		t.Fatalf("a ledger outage must be contained under fail_open, got %v", err)
	}

	closed := NewHookManager().With(h, FailClosed)
	if _, err := closed.AfterTool(context.Background(), call, result); err == nil {
		t.Fatal("fail_closed would abort the turn on a ledger outage — the contrast this " +
			"test exists to keep visible")
	}
}
