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
	tool := &ShellExec{Approvals: NewShellApprovals(), Router: newStrictBoxRouter(&fakeBoxBackend{})}
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

// TestDestructiveShellPatterns is the D-12 / PROF-02 truth table for the advisory
// destructive-shell gate. The load-bearing fix: UNSET or EMPTY (incl.
// whitespace-only, which TrimSpace collapses to empty) falls back to the built-in
// defaults so the gate stays ACTIVE. Copying .env.example, whose
// AURA_SHELL_DESTRUCTIVE_PATTERNS line is commented out, therefore never disables
// the gate by accident (success criterion #2). Only an explicit, case-insensitive
// "off" opts out; any other non-empty value REPLACES the default set.
func TestDestructiveShellPatterns(t *testing.T) {
	const env = "AURA_SHELL_DESTRUCTIVE_PATTERNS"
	cases := []struct {
		name      string
		value     string
		set       bool
		probe     string
		wantMatch bool
	}{
		{name: "unset_copied_sample", set: false, probe: "rm -rf /", wantMatch: true},
		{name: "empty", value: "", set: true, probe: "rm -rf /", wantMatch: true},
		{name: "whitespace", value: "   ", set: true, probe: "rm -rf /", wantMatch: true},
		{name: "off_lower", value: "off", set: true, probe: "rm -rf /", wantMatch: false},
		{name: "off_upper", value: "OFF", set: true, probe: "rm -rf /", wantMatch: false},
		{name: "off_mixed", value: "Off", set: true, probe: "rm -rf /", wantMatch: false},
		{name: "custom_matches_own", value: `rm -rf /tmp/x,mkfs`, set: true, probe: "rm -rf /tmp/x", wantMatch: true},
		{name: "custom_replaces_default", value: `rm -rf /tmp/x,mkfs`, set: true, probe: "rm -rf /", wantMatch: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(env, tc.value)
			} else {
				os.Unsetenv(env)
			}
			ok, err := destructiveShellMatch(tc.probe)
			if err != nil {
				t.Fatalf("destructiveShellMatch(%q): %v", tc.probe, err)
			}
			if ok != tc.wantMatch {
				t.Errorf("env set=%v value=%q: destructiveShellMatch(%q) = %v, want gate active=%v",
					tc.set, tc.value, tc.probe, ok, tc.wantMatch)
			}
		})
	}
}
