package tools

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestShellExecDestructivePatternRequiresApproval(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\brm\s+-rf\b`)
	tool := &ShellExec{Approvals: NewShellApprovals()}
	ctx := ctxWith(t, "sess-shell-gate", "call-shell-gate")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"rm -rf /tmp/aura-never-run"}`))
	if err != nil {
		t.Fatalf("Execute should return a structured result, got error: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("preview missing approval error: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, `"command_sha256"`) {
		t.Fatalf("preview missing command digest: %q", res.Preview)
	}
	payload := approvalPayload(t, res.Preview)
	if payload.Command != "rm -rf /tmp/aura-never-run" {
		t.Fatalf("payload command = %q", payload.Command)
	}
	if payload.Question == "" || !strings.Contains(payload.Message, "question exactly equal") {
		t.Fatalf("payload missing host-rendered question instructions: %+v", payload)
	}
	challenge, ok := tool.Approvals.PendingChallenge("sess-shell-gate", payload.CommandSHA256)
	if !ok {
		t.Fatal("approval-required result did not create a pending host challenge")
	}
	if challenge.Question != payload.Question || challenge.Command != payload.Command || challenge.Cwd != payload.Cwd {
		t.Fatalf("payload did not match pending challenge: payload=%+v challenge=%+v", payload, challenge)
	}
	if res.Meta != nil {
		t.Fatalf("approval-required result must not carry execution Meta: %#v", *res.Meta)
	}
}

func TestShellExecDestructiveBackgroundRequiresApproval(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\becho\s+danger\b`)
	tool := &ShellExec{Approvals: NewShellApprovals(), Background: NewBackgroundShells(nil)}
	ctx := ctxWith(t, "sess-shell-bg-gate", "call-shell-bg-gate")

	res, err := tool.Execute(ctx, mustJSON(t, map[string]any{
		"command":    "echo danger",
		"background": true,
	}))
	if err != nil {
		t.Fatalf("Execute should return a structured result, got error: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("background destructive command should require approval: %q", res.Preview)
	}
	if res.Meta != nil {
		t.Fatalf("unapproved background command must not start a shell: %#v", *res.Meta)
	}
}

func TestShellExecDestructiveApprovalIsOneShot(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\becho\s+danger\b`)
	approvals := NewShellApprovals()
	tool := &ShellExec{Approvals: approvals}
	ctx := ctxWith(t, "sess-shell-ok", "call-shell-ok")
	command := "echo danger"
	digest := ShellApprovalDigest(command, "")
	approvals.Approve("sess-shell-ok", digest)

	res, err := tool.Execute(ctx, mustJSON(t, map[string]string{"command": command}))
	if err != nil {
		t.Fatalf("approved Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "danger") {
		t.Fatalf("approved command did not run: %q", res.Preview)
	}

	res, err = tool.Execute(ctx, mustJSON(t, map[string]string{"command": command}))
	if err != nil {
		t.Fatalf("second Execute should return a structured result, got error: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("second identical command should require a new approval: %q", res.Preview)
	}
}

func TestShellApprovalConcurrentConsumeIsOneShot(t *testing.T) {
	approvals := NewShellApprovals()
	digest := ShellApprovalDigest("echo once", "")
	approvals.Approve("sess-concurrent", digest)

	var successes int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if approvals.Consume("sess-concurrent", digest) {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("Consume successes = %d, want exactly 1", successes)
	}
}

func TestShellExecDestructiveApprovalIsBoundToExactCwd(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\becho\s+cwd-bound\b`)
	approvals := NewShellApprovals()
	tool := &ShellExec{Approvals: approvals}
	ctx := ctxWith(t, "sess-shell-cwd", "call-shell-cwd")
	command := "echo cwd-bound"
	exactCwd := t.TempDir()
	wrongCwd := t.TempDir()

	approvals.Approve("sess-shell-cwd", ShellApprovalDigest(command, wrongCwd))
	res, err := tool.Execute(ctx, mustJSON(t, map[string]string{"command": command, "cwd": exactCwd}))
	if err != nil {
		t.Fatalf("wrong-cwd Execute should return structured result: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("approval for wrong cwd should not run command: %q", res.Preview)
	}

	approvals.Approve("sess-shell-cwd", ShellApprovalDigest(command, exactCwd))
	res, err = tool.Execute(ctx, mustJSON(t, map[string]string{"command": command, "cwd": exactCwd}))
	if err != nil {
		t.Fatalf("exact-cwd Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "cwd-bound") {
		t.Fatalf("approval for exact cwd did not run: %q", res.Preview)
	}
}

func TestShellExecDestructiveApprovalUsesNormalizedCRLFDigest(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\becho\s+crlf\b`)
	approvals := NewShellApprovals()
	tool := &ShellExec{Approvals: approvals}
	ctx := ctxWith(t, "sess-shell-crlf-digest", "call-shell-crlf-digest")
	rawCommand := "echo crlf\r\n"
	normalizedCommand := strings.ReplaceAll(rawCommand, "\r\n", "\n")
	approvals.Approve("sess-shell-crlf-digest", ShellApprovalDigest(normalizedCommand, ""))

	res, err := tool.Execute(ctx, mustJSON(t, map[string]string{"command": rawCommand}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "crlf") || strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("normalized digest did not approve raw CRLF command: %q", res.Preview)
	}
}

func TestShellApprovalDigestDoesNotTrimInputs(t *testing.T) {
	if ShellApprovalDigest("echo trim", "") == ShellApprovalDigest(" echo trim", "") {
		t.Fatal("command leading space must affect digest")
	}
	if ShellApprovalDigest("echo trim", "cwd") == ShellApprovalDigest("echo trim", " cwd") {
		t.Fatal("cwd leading space must affect digest")
	}
}

func TestShellExecDestructivePatternNilApprovalsFailsClosed(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\becho\s+danger\b`)
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-shell-nil", "call-shell-nil")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo danger"}`))
	if err != nil {
		t.Fatalf("Execute should return a structured result, got error: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("nil approvals should fail closed for configured destructive matches: %q", res.Preview)
	}
}

func TestShellExecDestructivePatternInvalidRegexReturnsError(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `[`)
	tool := &ShellExec{Approvals: NewShellApprovals()}
	ctx := ctxWith(t, "sess-shell-bad-pattern", "call-shell-bad-pattern")

	_, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo hello"}`))
	if err == nil {
		t.Fatal("Execute returned nil error for invalid destructive pattern")
	}
	if !strings.Contains(err.Error(), "AURA_SHELL_DESTRUCTIVE_PATTERNS") {
		t.Fatalf("error should name the bad env var, got: %v", err)
	}
}

type shellApprovalPayload struct {
	Error         string `json:"error"`
	CommandSHA256 string `json:"command_sha256"`
	Command       string `json:"command"`
	Cwd           string `json:"cwd"`
	Question      string `json:"question"`
	Message       string `json:"message"`
}

func approvalPayload(t *testing.T, preview string) shellApprovalPayload {
	t.Helper()
	var payload shellApprovalPayload
	if err := json.Unmarshal([]byte(preview), &payload); err != nil {
		t.Fatalf("approval payload is not JSON: %v; preview=%q", err, preview)
	}
	if payload.Error != "shell_approval_required" {
		t.Fatalf("payload error = %q", payload.Error)
	}
	return payload
}
