package tools

import (
	"encoding/json"
	"strings"
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
	if res.Meta != nil {
		t.Fatalf("approval-required result must not carry execution Meta: %#v", *res.Meta)
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
