// Wiring the verification ledger into a turn.
//
// The ledger only means something if something writes to it, and Hermes writes from two
// places: its terminal tool records a finished command, and its file tools mark the
// workspace edited. Aura has the same two events on the Hook surface it already exposes
// (hooks.go), so this is a Hook rather than a change to any shipped tool -- the tool
// results already carry everything needed. shell_exec's Meta has cwd and exit_code
// (shell_exec_sandbox.go), and a write tool's path is in the call arguments.
//
// FailOpen is the only sane policy here: a ledger that cannot be written must never
// abort the turn it was observing.
package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// writeToolPathArgs names the tools whose call arguments carry the path they modified.
// Adding a write tool without adding it here means its edits do not stale the evidence,
// so the gate would call a workspace verified after it changed -- the one failure mode
// this whole ledger exists to prevent.
var writeToolPathArgs = map[string]string{
	"write_file": "path",
	"patch":      "path",
}

// writeToolPath returns the path a write tool's arguments name. ok=false when the call
// is not a write tool, its arguments do not parse, or it names no path -- three ways of
// having nothing to record. The run loop's own edited-path accumulator
// (recordEditedPath) reads through here too, so the gate and the ledger can never
// disagree about which tools edit and where they say so.
func writeToolPath(call llm.ToolCall) (string, bool) {
	field, isWrite := writeToolPathArgs[call.Function.Name]
	if !isWrite {
		return "", false
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", false
	}
	path, _ := args[field].(string)
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}

// VerificationHook records verification evidence and edit staleness as a turn runs.
type VerificationHook struct {
	Store      *EvidenceStore
	Detector   FilesystemProjectDetector
	IdentityID string
	SessionID  string
}

// BeforeModel is a no-op: this hook observes, it never steers a request.
func (h *VerificationHook) BeforeModel(context.Context, *llm.Request) (*ModelHookResult, error) {
	return nil, nil
}

// BeforeTool is a no-op: the ledger records what a call DID, so it has nothing to say
// before one runs.
func (h *VerificationHook) BeforeTool(context.Context, llm.ToolCall) (*ToolHookResult, error) {
	return nil, nil
}

// OnTurnEnd is a no-op: the gate reads the ledger from the loop's completion seam,
// where it can still act on what it finds. A hook that only returns error cannot.
func (h *VerificationHook) OnTurnEnd(context.Context, HookTurn) error { return nil }

// AfterTool is the whole hook. A finished shell command may be verification evidence; a
// finished write stales it.
func (h *VerificationHook) AfterTool(
	ctx context.Context,
	call llm.ToolCall,
	result tools.ToolResult,
) (*ToolResultHookResult, error) {
	if h == nil || h.Store == nil {
		return nil, nil
	}
	switch call.Function.Name {
	case "shell_exec":
		h.recordShell(ctx, call, result)
	default:
		h.markEdited(ctx, call)
	}
	// Always nil: this hook never rewrites a tool result.
	return nil, nil
}

func (h *VerificationHook) recordShell(ctx context.Context, call llm.ToolCall, result tools.ToolResult) {
	var args struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || strings.TrimSpace(args.Command) == "" {
		return
	}
	// A backgrounded command has not finished, so it has proved nothing yet.
	if args.Background {
		return
	}
	cwd, exitCode, ok := shellOutcome(result)
	if !ok {
		return
	}
	facts := h.Detector.ProjectFactsFor(cwd)
	if !facts.Found {
		return
	}
	if _, _, err := h.Store.RecordTerminalResult(ctx, h.IdentityID, ClassifyVerificationCommandInput{
		Command: args.Command, CWD: cwd, Root: facts.Root,
		SessionID: h.SessionID, ExitCode: exitCode,
		Output: result.Preview, VerifyCommands: facts.VerifyCommands,
	}); err != nil {
		slog.Warn("verification ledger: record terminal result", "error", err, "session_id", h.SessionID)
	}
}

func (h *VerificationHook) markEdited(ctx context.Context, call llm.ToolCall) {
	path, ok := writeToolPath(call)
	if !ok {
		return
	}
	facts := h.Detector.ProjectFactsFor(filepath.Dir(path))
	if !facts.Found {
		return
	}
	if err := h.Store.MarkWorkspaceEdited(ctx, h.IdentityID, h.SessionID, facts.Root, []string{path}); err != nil {
		slog.Warn("verification ledger: mark workspace edited", "error", err, "session_id", h.SessionID)
	}
}

// shellOutcome reads the cwd and exit code shell_exec publishes in its result Meta. A
// command with no exit code did not run to completion (an approval gate, a timeout kill)
// and is not evidence of anything.
func shellOutcome(result tools.ToolResult) (cwd string, exitCode int, ok bool) {
	if result.Meta == nil {
		return "", 0, false
	}
	meta := *result.Meta
	cwd, _ = meta["cwd"].(string)
	if strings.TrimSpace(cwd) == "" {
		return "", 0, false
	}
	switch code := meta["exit_code"].(type) {
	case int:
		return cwd, code, true
	case int64:
		return cwd, int(code), true
	case float64:
		return cwd, int(code), true
	}
	return "", 0, false
}
