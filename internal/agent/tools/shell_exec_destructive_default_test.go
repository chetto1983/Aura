package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// B-10: the destructive-shell gate is an ADVISORY guardrail that must be ON by
// default. With AURA_SHELL_DESTRUCTIVE_PATTERNS UNSET, a clearly-destructive
// command (rm -rf /) must still trip the approval gate — the operator did not
// have to opt in. (It is advisory, not a security boundary: a determined model
// can phrase around it; the real boundary is the host/sandbox policy.)
func TestDestructiveShellDefaultOnFlagsRmRf(t *testing.T) {
	os.Unsetenv("AURA_SHELL_DESTRUCTIVE_PATTERNS")
	tool := &ShellExec{Approvals: NewShellApprovals()}
	ctx := ctxWith(t, "sess-default-gate", "call-default-gate")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"rm -rf /"}`))
	if err != nil {
		t.Fatalf("Execute should return a structured result, got error: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_approval_required") {
		t.Fatalf("default gate must flag a clearly-destructive command without opt-in: %q", res.Preview)
	}
}

// The default gate matches the conservative built-in set when unset.
func TestDestructiveShellDefaultPatternsCoverConservativeSet(t *testing.T) {
	os.Unsetenv("AURA_SHELL_DESTRUCTIVE_PATTERNS")
	destructive := []string{
		"rm -rf /",
		"rm -fr /usr",
		"sudo rm -rf /var",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		":(){ :|:& };:",
		"echo x > /dev/sda",
	}
	for _, cmd := range destructive {
		ok, err := destructiveShellMatch(cmd)
		if err != nil {
			t.Fatalf("destructiveShellMatch(%q): %v", cmd, err)
		}
		if !ok {
			t.Errorf("default conservative set should flag %q", cmd)
		}
	}

	benign := []string{
		"ls -la",
		"go test ./...",
		"rm /tmp/one-file.txt",
		"git status",
		"python3 build.py",
	}
	for _, cmd := range benign {
		ok, err := destructiveShellMatch(cmd)
		if err != nil {
			t.Fatalf("destructiveShellMatch(%q): %v", cmd, err)
		}
		if ok {
			t.Errorf("default conservative set must not flag benign %q (would break legit commands)", cmd)
		}
	}
}

// The gate stays advisory + overridable: an explicit empty/off value disables it,
// and an explicit custom set replaces the default (no merge surprises).
func TestDestructiveShellDefaultIsOverridable(t *testing.T) {
	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", "off")
	if ok, err := destructiveShellMatch("rm -rf /"); err != nil || ok {
		t.Fatalf(`explicit "off" must disable the gate: ok=%v err=%v`, ok, err)
	}

	t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", `(?i)\bcustom-danger\b`)
	if ok, err := destructiveShellMatch("rm -rf /"); err != nil || ok {
		t.Fatalf("an explicit custom set must replace the default, not merge: ok=%v err=%v", ok, err)
	}
	if ok, err := destructiveShellMatch("run custom-danger now"); err != nil || !ok {
		t.Fatalf("explicit custom set must match its own pattern: ok=%v err=%v", ok, err)
	}
}
