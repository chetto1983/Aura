package tools

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellExecSpecIsNotDeferred(t *testing.T) {
	s := (&ShellExec{}).Spec()
	if s.Name != "shell_exec" {
		t.Fatalf("name = %q, want shell_exec", s.Name)
	}
	// Keystone tool: the model must always see it + its arg schema (like sandbox_exec).
	if s.Deferred {
		t.Fatal("shell_exec must NOT be deferred — the model needs the arg schema")
	}
}

func TestShellExecRunsCommand(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo hello-aura"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "hello-aura") {
		t.Fatalf("preview missing stdout: %q", res.Preview)
	}
}

func TestShellExecReportsExitCode(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	// `exit 7` runs identically under `/bin/sh -c` and `cmd /c`.
	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"exit 7"}`))
	if err != nil {
		t.Fatalf("a failing command is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[exit code 7]") {
		t.Fatalf("preview missing exit-code marker: %q", res.Preview)
	}
}

// shellIsCmd reports whether the resolved Windows shell is the degraded cmd.exe
// fallback (no POSIX bash found) — the syntax the tests feed depends on it.
func shellIsCmd() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	name, _ := shellInvocation("true")
	base := strings.ToLower(filepath.Base(name))
	return base == "cmd" || base == "cmd.exe"
}

func TestShellExecPassesEnv(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	cmd := `echo "$AURA_SHELL_TEST"`
	if shellIsCmd() {
		cmd = "echo %AURA_SHELL_TEST%"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, Env: map[string]string{"AURA_SHELL_TEST": "marker-42"}})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "marker-42") {
		t.Fatalf("env not passed through to command: %q", res.Preview)
	}
}

func TestShellExecHonorsCwd(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")
	dir := t.TempDir()

	cmd := "pwd"
	if shellIsCmd() {
		cmd = "cd"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, Cwd: dir})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, filepath.Base(dir)) {
		t.Fatalf("cwd %q not honored, output: %q", dir, res.Preview)
	}
}

func TestShellExecTimesOut(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	cmd := "sleep 3"
	if shellIsCmd() {
		cmd = "ping -n 4 127.0.0.1"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, TimeoutMs: 200})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("a timeout is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("preview missing timeout marker: %q", res.Preview)
	}
}

// TestShellExecCwdPersistsAcrossCalls: Bash-tool parity — a `cd` in one call
// carries into the next call of the SAME session; a different session still starts
// at the workspace root; the tracking marker never leaks into the model-visible
// output. Skipped under the degraded cmd.exe fallback (no tracking there).
func TestShellExecCwdPersistsAcrossCalls(t *testing.T) {
	if shellIsCmd() {
		t.Skip("cmd.exe fallback: cwd tracking is POSIX-only (degraded mode)")
	}
	root := t.TempDir()
	tool := &ShellExec{WorkspaceRoot: root}

	res, err := tool.Execute(ctxWith(t, "sess-cwd", "call-1"),
		json.RawMessage(`{"command":"mkdir -p subdir && cd subdir && echo moved"}`))
	if err != nil {
		t.Fatalf("Execute(cd): %v", err)
	}
	if strings.Contains(res.Preview, cwdMarker) {
		t.Fatalf("tracking marker leaked into output: %q", res.Preview)
	}

	res, err = tool.Execute(ctxWith(t, "sess-cwd", "call-2"),
		json.RawMessage(`{"command":"pwd -W 2>/dev/null || pwd"}`))
	if err != nil {
		t.Fatalf("Execute(pwd): %v", err)
	}
	if !strings.Contains(res.Preview, "subdir") {
		t.Fatalf("cwd did not persist across calls: pwd = %q (want .../subdir)", res.Preview)
	}

	// A DIFFERENT session must not inherit the first session's cd.
	res, err = tool.Execute(ctxWith(t, "sess-other", "call-3"),
		json.RawMessage(`{"command":"pwd -W 2>/dev/null || pwd"}`))
	if err != nil {
		t.Fatalf("Execute(pwd other session): %v", err)
	}
	if strings.Contains(res.Preview, "subdir") {
		t.Fatalf("cwd LEAKED across sessions: pwd = %q", res.Preview)
	}
}

func TestExtractCwdMarker(t *testing.T) {
	clean, dir := extractCwdMarker("hello\n" + cwdMarker + " D:/work\n")
	if clean != "hello" || dir != "D:/work" {
		t.Fatalf("extract = (%q, %q), want (hello, D:/work)", clean, dir)
	}
	clean, dir = extractCwdMarker("no marker here")
	if clean != "no marker here" || dir != "" {
		t.Fatalf("no-marker extract = (%q, %q)", clean, dir)
	}
}

func TestShellExecRejectsBadArgs(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "malformed JSON", raw: json.RawMessage(`{`), want: "shell_exec args"},
		{name: "empty command", raw: json.RawMessage(`{"command":"   "}`), want: "command is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tool.Execute(ctx, tt.raw); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want substring %q", err, tt.want)
			}
		})
	}
}
